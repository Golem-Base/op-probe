package withdraw_cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Golem-Base/op-probe/internal"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	opNodeBindings "github.com/ethereum-optimism/optimism/op-node/bindings"
	bindingspreview "github.com/ethereum-optimism/optimism/op-node/bindings/preview"
	opNodePreviewBindings "github.com/ethereum-optimism/optimism/op-node/bindings/preview"
	"github.com/ethereum-optimism/optimism/op-node/withdrawals"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient/gethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var ProveCommand = &cli.Command{
	Name:  "prove",
	Usage: "Prove a withdrawal transaction",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "private-key",
			Usage:    "Private key of address to send test transaction from",
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
			Name:     "tx",
			Usage:    "The L2 withdrawal transaction hash",
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

		privateKey, err := crypto.HexToECDSA(c.String("private-key"))
		if err != nil {
			return fmt.Errorf("failed to parse private-key: %w", err)
		}

		l1RpcUrl := c.String("l1-rpc-url")
		l1Client, l1ChainId, err := internal.ConnectClient(ctx, l1RpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", l1RpcUrl, err)
		}

		l2RpcUrl := c.String("l2-rpc-url")
		l2Client, _, err := internal.ConnectClient(ctx, l2RpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", l2RpcUrl, err)
		}

		withdrawalTxHash := common.HexToHash(c.String("tx"))

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

		withdrawalTxReceipt, err := l2Client.TransactionReceipt(ctx, withdrawalTxHash)
		if err != nil {
			return fmt.Errorf("could not get receipt for withdrawal event in transaction: %s: %w", withdrawalTxHash.Hex(), err)
		}

		game, err := withdrawals.FindLatestGame(ctx, &disputeGameFactory.DisputeGameFactoryCaller, &optimismPortal.OptimismPortal2Caller)
		if err != nil {
			return fmt.Errorf("failed to find latest game: %w", err)
		}

		gameL2BlockNumber := new(big.Int).SetBytes(game.ExtraData[0:32])

		if gameL2BlockNumber.Uint64() < withdrawalTxReceipt.BlockNumber.Uint64() {
			return fmt.Errorf("game for this withdrawal has not been proposed yet, %d blocks remaining", withdrawalTxReceipt.BlockNumber.Uint64()-gameL2BlockNumber.Uint64())
		}

		params, err := withdrawals.ProveWithdrawalParametersFaultProofs(
			ctx,
			gethclient.New(l2Client.Client()),
			l2Client,
			l2Client,
			withdrawalTxHash,
			&disputeGameFactory.DisputeGameFactoryCaller,
			&optimismPortal.OptimismPortal2Caller,
		)
		if err != nil {
			return fmt.Errorf("could not generate fault proofs for withdrawal: %w", err)
		}

		// log.Info("constructed fault proof parameters", params.WithdrawalProof)

		opts, err := bind.NewKeyedTransactorWithChainID(privateKey, l1ChainId)
		if err != nil {
			return fmt.Errorf("could not setup transactor: %w", err)
		}

		tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
			return optimismPortal.ProveWithdrawalTransaction(
				opts,
				bindingspreview.TypesWithdrawalTransaction{
					Nonce:    params.Nonce,
					Sender:   params.Sender,
					Target:   params.Target,
					Value:    params.Value,
					GasLimit: params.GasLimit,
					Data:     params.Data,
				},
				params.L2OutputIndex,
				bindingspreview.TypesOutputRootProof{
					Version:                  params.OutputRootProof.Version,
					StateRoot:                params.OutputRootProof.StateRoot,
					MessagePasserStorageRoot: params.OutputRootProof.MessagePasserStorageRoot,
					LatestBlockhash:          params.OutputRootProof.LatestBlockhash,
				},
				params.WithdrawalProof,
			)
		})
		if err != nil {
			return fmt.Errorf("failed to prove withdrawal transaction: %w", err)
		}

		receipt, err := wait.ForReceiptOK(ctx, l1Client, tx.Hash())
		if err != nil {
			if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
				log.Error("transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
				return fmt.Errorf("failure in transaction execution: %w", err)
			} else {
				return fmt.Errorf("failed to get transaction receipt: %w", err)
			}
		}

		log.Info("successfully proven withdrawal transaction", "receipt", receipt)

		return nil
	},
}
