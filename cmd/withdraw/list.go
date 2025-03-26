package withdraw_cmd

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Golem-Base/op-probe/bindings"
	"github.com/Golem-Base/op-probe/internal"
	e2eBindings "github.com/ethereum-optimism/optimism/op-e2e/bindings"
	opNodeBindings "github.com/ethereum-optimism/optimism/op-node/bindings"
	opNodePreviewBindings "github.com/ethereum-optimism/optimism/op-node/bindings/preview"
	"github.com/ethereum-optimism/optimism/op-node/withdrawals"
	"github.com/ethereum-optimism/optimism/op-service/predeploys"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

type WithdrawalStatus int

const (
	Initialized WithdrawalStatus = iota
	Provable
	Proven
	Finalized
)

var WithdrawalStatusName = map[WithdrawalStatus]string{
	Initialized: "initialized",
	Provable:    "provable",
	Proven:      "proven",
	Finalized:   "finalized",
}

var ListCommand = &cli.Command{
	Name:  "list",
	Usage: "Lists all ongoing withdrawals and their statuses (permissioned game)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "account",
			Usage:    "account to check for previous withdrawals",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "l1-rpc-url",
			Usage:    "Url for L1 execution client",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "l2-rpc-url",
			Usage:    "Url for L2 execution client",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "dispute-game-factory-address",
			Usage:    "Contract address for DisputeGameFactory (* or proxy)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "optimism-portal-address",
			Usage:    "Contract address for OptimismPortal (* or proxy)",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()

		l1RpcUrl := c.String("l1-rpc-url")
		l1Client, _, err := internal.ConnectClient(ctx, l1RpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", l1RpcUrl, err)
		}

		l2RpcUrl := c.String("l2-rpc-url")
		l2Client, _, err := internal.ConnectClient(ctx, l2RpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", l1RpcUrl, err)
		}

		account, err := internal.SafeParseAddress(c.String("account"))
		if err != nil {
			return fmt.Errorf("could not parse account: %w", err)
		}

		disputeGameFactoryAddress, err := internal.SafeParseAddress(c.String("dispute-game-factory-address"))
		if err != nil {
			return fmt.Errorf("could not parse DisputeGameFactory address: %w", err)
		}
		disputeGameFactory, err := opNodeBindings.NewDisputeGameFactory(disputeGameFactoryAddress, l1Client)
		if err != nil {
			return fmt.Errorf("could not instantiate DisputeGameFactory contract: %w", err)
		}

		optimismPortalAddress, err := internal.SafeParseAddress(c.String("optimism-portal-address"))
		if err != nil {
			return fmt.Errorf("could not parse OptimismPortal address: %w", err)
		}
		optimismPortal, err := opNodePreviewBindings.NewOptimismPortal2(optimismPortalAddress, l1Client)
		if err != nil {
			return fmt.Errorf("could not instantiate OptimismPortal contract: %w", err)
		}

		l2ToL1MessagePasserFilterer, err := e2eBindings.NewL2ToL1MessagePasserFilterer(predeploys.L2ToL1MessagePasserAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not instantiate L2ToL2MessagePasser filterer")
		}

		permissionedDisputeGameAddress, err := disputeGameFactory.GameImpls(&bind.CallOpts{}, 1)
		if err != nil {
			return fmt.Errorf("could not fetch game implementation: %w", err)
		}
		if permissionedDisputeGameAddress == internal.ZeroAddress {
			return fmt.Errorf("PermissionedDisputeGame not set on DisputeGameFactory contract")
		}
		permissionedDisputeGame, err := bindings.NewPermissionedDisputeGame(permissionedDisputeGameAddress, l1Client)
		if err != nil {
			return fmt.Errorf("could not instantiate PermissionedDisputeGame contract: %w", err)
		}

		proposerAddress, err := permissionedDisputeGame.Proposer(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("was not able to fetch proposer: %w", err)
		}

		log.Info("permissionedDisputeGameAddress", "address", permissionedDisputeGameAddress)

		game, err := withdrawals.FindLatestGame(ctx, &disputeGameFactory.DisputeGameFactoryCaller, &optimismPortal.OptimismPortal2Caller)
		if err != nil {
			return fmt.Errorf("failed to find latest game: %w", err)
		}

		gameL2BlockNumber := new(big.Int).SetBytes(game.ExtraData[0:32])

		log.Info("Found latest game", "game", game.Index, "l2Block", gameL2BlockNumber, "timestamp", time.Unix(int64(game.Timestamp), 0))

		// TODO This is possibly naïve behaviour and might need better filtering to identify withdrawals specifically
		iterator, err := l2ToL1MessagePasserFilterer.FilterMessagePassed(&bind.FilterOpts{Context: ctx, Start: 0, End: nil}, nil, []common.Address{account}, []common.Address{account})

		for iterator.Next() {
			status := Initialized

			event := iterator.Event

			block, err := l2Client.BlockByNumber(ctx, big.NewInt(int64(event.Raw.BlockNumber)))
			if err != nil {
				return fmt.Errorf("failed to get block %d: %w", event.Raw.BlockNumber, err)
			}

			valueInEth := new(big.Float).Quo(
				new(big.Float).SetInt(event.Value),
				new(big.Float).SetInt(big.NewInt(1e18)),
			)

			if gameL2BlockNumber.Uint64() > block.Number().Uint64() {
				status = Provable
			}
			if status == Provable {
				proven, err := optimismPortal.ProvenWithdrawals(&bind.CallOpts{}, event.WithdrawalHash, proposerAddress)
				if err != nil {
					return fmt.Errorf("could not fetch proven withdrawal: %w", err)
				}

				if proven.DisputeGameProxy != common.BytesToAddress([]byte{0}) {
					status = Proven
				}
			}

			timestamp := time.Unix(int64(block.Time()), 0)

			nonce := DecodeVersionedNonce(event.Nonce)

			withdrawalHash := common.Bytes2Hex(event.WithdrawalHash[:])

			log.Info(fmt.Sprintf("Withdrawal: %s", nonce),
				"initialized", timestamp,
				"block", event.Raw.BlockNumber,
				"withdrawalHash", withdrawalHash,
				"txHash", event.Raw.TxHash.Hex(),
				"amount", valueInEth,
				"status", WithdrawalStatusName[status],
			)
		}
		if err := iterator.Error(); err != nil {
			return fmt.Errorf("Found error while iterating through events: %w", err)
		}

		return nil
	},
}

func DecodeVersionedNonce(nonce *big.Int) *big.Int {
	mask := new(big.Int).Sub(
		new(big.Int).Lsh(big.NewInt(1), 240),
		big.NewInt(1),
	)

	nonceValue := new(big.Int).And(nonce, mask)

	return nonceValue
}
