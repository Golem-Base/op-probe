package withdraw_cmd

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Golem-Base/op-probe/bindings"
	"github.com/Golem-Base/op-probe/internal"
	e2eBindings "github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/receipts"
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
	ClaimResolved
	GameResolved
	Finalized
)

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

		l2StandardBridgeFilterer, err := e2eBindings.NewL2StandardBridgeFilterer(predeploys.L2StandardBridgeAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not instantiate L2StandardBridge filterer")
		}

		permissionedDisputeGameAddress, err := disputeGameFactory.GameImpls(&bind.CallOpts{}, 1)
		if err != nil {
			return fmt.Errorf("could not fetch game implementation: %w", err)
		}
		if permissionedDisputeGameAddress == internal.ZeroAddress {
			return fmt.Errorf("PermissionedDisputeGame not set on DisputeGameFactory contract")
		}

		l2ToL1MessagePasser, err := e2eBindings.NewL2ToL1MessagePasser(predeploys.L2ToL1MessagePasserAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not not instantiate L2ToL1MessagePasser contract: %w", err)
		}

		proofMaturityDelaySeconds, err := optimismPortal.ProofMaturityDelaySeconds(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("could not call OptimismPortal.ProofMaturityDelaySeconds: %w", err)
		}

		game, err := withdrawals.FindLatestGame(ctx, &disputeGameFactory.DisputeGameFactoryCaller, &optimismPortal.OptimismPortal2Caller)
		if err != nil {
			return fmt.Errorf("failed to find latest game: %w", err)
		}

		gameL2BlockNumber := new(big.Int).SetBytes(game.ExtraData[0:32])

		log.Info("Found latest game", "game", game.Index, "l2Block", gameL2BlockNumber, "timestamp", time.Unix(int64(game.Timestamp), 0))

		iterator, err := l2StandardBridgeFilterer.FilterWithdrawalInitiated(
			&bind.FilterOpts{Context: ctx, Start: 0, End: nil},
			[]common.Address{internal.ZeroAddress},
			[]common.Address{predeploys.LegacyERC20ETHAddr},
			[]common.Address{account},
		)
		for iterator.Next() {

			status := Initialized

			event := iterator.Event

			receipt, err := l2Client.TransactionReceipt(ctx, event.Raw.TxHash)
			if err != nil {
				return fmt.Errorf("could not get receipt for withdrawal event in transaction: %s: %w", event.Raw.TxHash.Hex(), err)
			}

			messagePassedEvent, err := receipts.FindLog(receipt.Logs, l2ToL1MessagePasser.ParseMessagePassed)
			if err != nil {
				return fmt.Errorf("could not parse L2ToL1MessagePasser.MessagePassed event from the receipt logs: %w", err)
			}

			if gameL2BlockNumber.Uint64() >= receipt.BlockNumber.Uint64() {
				status = Provable
			}

			timestamp := uint64(0)
			var created_at_time time.Time
			disputeGameStatus := uint8(0)
			isClaimResolved := false
			challengerDuration := time.Duration(0)
			maxClockDuration := time.Duration(0)

			if status == Provable {
				proven, err := optimismPortal.ProvenWithdrawals(
					&bind.CallOpts{},
					messagePassedEvent.WithdrawalHash,
					account, // TODO This is a simplified lookup and a more robust approach would be to filter by event for WithdrawalProven events
				)
				if err != nil {
					return fmt.Errorf("could not fetch proven withdrawal: %w", err)
				}

				if proven.DisputeGameProxy != common.BytesToAddress([]byte{0}) {
					status = Proven
					timestamp = proven.Timestamp

					permissionedDisputeGame, err := bindings.NewPermissionedDisputeGame(proven.DisputeGameProxy, l1Client)
					if err != nil {
						return fmt.Errorf("could not construct permissioned dispute game")
					}

					created_at, err := permissionedDisputeGame.CreatedAt(&bind.CallOpts{})
					if err != nil {
						return fmt.Errorf("could not fetch DisputeGame.CreatedAt: %w", err)
					}
					created_at_time = time.Unix(int64(created_at), 0)

					disputeGameStatus, err = permissionedDisputeGame.Status(&bind.CallOpts{})
					if err != nil {
						return fmt.Errorf("could not fetch DisputeGame.Status: %w", err)
					}

					_maxClockDuration, err := permissionedDisputeGame.MaxClockDuration(&bind.CallOpts{})
					if err != nil {
						return fmt.Errorf("PermissionedDisputeGame.GetChallengerDuration failed: %w", err)
					}
					maxClockDuration = time.Duration(_maxClockDuration * uint64(time.Second))

					_challengerDuration, err := permissionedDisputeGame.GetChallengerDuration(&bind.CallOpts{}, common.Big0)
					if err != nil {
						return fmt.Errorf("PermissionedDisputeGame.GetChallengerDuration failed: %w", err)
					}
					challengerDuration = time.Duration(_challengerDuration * uint64(time.Second))

					isClaimResolved, err = permissionedDisputeGame.ResolvedSubgames(&bind.CallOpts{}, common.Big0)
					if err != nil {
						return fmt.Errorf("PermissionedDisputeGame.ResolvedSubgame failed: %w", err)
					}

					if isClaimResolved {
						status = ClaimResolved
					}

				}
			}

			nonce := DecodeVersionedNonce(messagePassedEvent.Nonce)

			withdrawalHash := common.Bytes2Hex(messagePassedEvent.WithdrawalHash[:])
			provenTime := time.Unix(int64(timestamp), 0)
			finalizableTime := time.Unix(int64(timestamp)+proofMaturityDelaySeconds.Int64(), 0)

			secondsUntilWithdrawalFinalization := time.Duration(0)
			if status == Proven {
				secondsUntilWithdrawalFinalization = finalizableTime.Sub(time.Now())
			}

			log.Info(fmt.Sprintf("Withdrawal: %s", nonce),
				"from", event.From,
				"to", event.To,
				"l1Token", event.L1Token,
				"l2Token", event.L2Token,
				"amount", internal.FormatWei(event.Amount),
				"block", receipt.BlockNumber.Uint64(),
				"withdrawalHash", withdrawalHash,
				"transactionHash", event.Raw.TxHash.Hex(),
				"status", status,
				"timestamp_proven", provenTime,
				"timestamp_created_at", created_at_time,
				"timestamp_finalizable", finalizableTime,
				"finalizable_in", secondsUntilWithdrawalFinalization,
				"proof_maturity_delay", time.Duration(proofMaturityDelaySeconds.Int64()*int64(time.Second)),
				"isClaimResolved", isClaimResolved,
				"challengerDuration", challengerDuration,
				"maxClockDuration", maxClockDuration,
				"disputeGameStatus", disputeGameStatus,
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
