package withdraw_cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/Golem-Base/op-probe/bindings"
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

// TODO The `resolveClaim()` and `resolve()` functionality would be called by the challenger service so	it may be advisable to add a flag to wait for the challenger
var FinalizeCommand = &cli.Command{
	Name:  "finalize",
	Usage: "Finalizes a withdrawal transaction, assumes the private-key is the prover",
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

		account := crypto.PubkeyToAddress(privateKey.PublicKey)

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

		preBalance, err := l1Client.BalanceAt(ctx, account, nil)
		if err != nil {
			return fmt.Errorf("could not fetch balance: %w", err)
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

		messagePassedEvent, err := withdrawals.ParseMessagePassed(withdrawalTxReceipt)
		if err != nil {
			return fmt.Errorf("could not parse the MessagePassed event from the withdrawal transaction hash")
		}
		proven, err := optimismPortal.ProvenWithdrawals(
			&bind.CallOpts{},
			messagePassedEvent.WithdrawalHash,
			account,
		)
		if err != nil {
			return fmt.Errorf("could not fetch proven withdrawal: %w", err)
		}
		provenTimestamp := time.Unix(int64(proven.Timestamp), 0)
		if proven.Timestamp == 0 {
			return fmt.Errorf("withdrawal has not been previously proven")
		}

		log.Info("withdrawal has been proven",
			"proved_at", time.Unix(int64(proven.Timestamp), 0),
		)

		opts, err := bind.NewKeyedTransactorWithChainID(privateKey, l1ChainId)
		if err != nil {
			return fmt.Errorf("could not setup transactor: %w", err)
		}

		permissionedDisputeGame, err := bindings.NewPermissionedDisputeGame(proven.DisputeGameProxy, l1Client)
		if err != nil {
			return fmt.Errorf("could not construct permissioned dispute game")
		}
		_maxClockDuration, err := permissionedDisputeGame.MaxClockDuration(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("PermissionedDisputeGame.GetChallengerDuration failed: %w", err)
		}
		maxClockDuration := time.Duration(_maxClockDuration * uint64(time.Second))

		isClaimResolved, err := permissionedDisputeGame.ResolvedSubgames(&bind.CallOpts{}, common.Big0)
		if err != nil {
			return fmt.Errorf("PermissionedDisputeGame.ResolvedSubgame failed: %w", err)
		}
		if !isClaimResolved {
			log.Info("PermissionedDisputeGame has not resolved any subgames")

			_challengerDuration, err := permissionedDisputeGame.GetChallengerDuration(&bind.CallOpts{}, common.Big0)
			if err != nil {
				return fmt.Errorf("PermissionedDisputeGame.GetChallengerDuration failed: %w", err)
			}
			challengerDuration := time.Duration(_challengerDuration * uint64(time.Second))

			if challengerDuration < maxClockDuration {
				log.Info("challenger duration period has not passed, exiting...",
					"challengerDuration", challengerDuration,
					"maxClockDuration", maxClockDuration,
				)
				return nil
			} else {
				log.Info("challenger duration period has passed, continuing...",
					"challengerDuration", challengerDuration,
					"maxClockDuration", maxClockDuration,
				)
			}

			tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
				return permissionedDisputeGame.ResolveClaim(opts, common.Big0, common.Big0)
			})
			if err != nil {
				return fmt.Errorf("failed to send PermissionedDisputeGame.ResolveClaim(): %w", err)
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

			log.Info("successfully executed PermissionedDisputeGame.Resolve, exiting...", "tx", receipt.TxHash.Hex())
			return nil
		} else {
			log.Info("PermissionedDisputeGame has already resolved subgames, continuing...")
		}

		disputeGameResolvedAt, err := permissionedDisputeGame.ResolvedAt(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("could not fetch DisputeGame.Status: %w", err)
		}
		disputeGameResolvedAtTime := time.Unix(int64(disputeGameResolvedAt), 0)

		if disputeGameResolvedAt == 0 {
			log.Info("disputeGame unresolved, calling PermissionedDisputeGame.Resolve()")

			tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
				return permissionedDisputeGame.Resolve(opts)
			})
			if err != nil {
				return fmt.Errorf("failed to send PermissionedDisputeGame.Resolve(): %w", err)
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

			log.Info("successfully executed PermissionedDisputeGame.Resolve(), exiting...", "tx", receipt.TxHash.Hex())
			return nil

		} else {
			disputeGameStatus, err := permissionedDisputeGame.Status(&bind.CallOpts{})
			if err != nil {
				return fmt.Errorf("could not fetch PermissionedDisputeGame.Status(): %w", err)
			}
			log.Info("PermissionedDisputeGame has been resolved, continuing...", "status", disputeGameStatus, "resolvedAt", time.Unix(int64(disputeGameResolvedAt), 0))
		}

		proofMaturityDelaySeconds, err := optimismPortal.ProofMaturityDelaySeconds(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("could not call OptimismPortal.ProofMaturityDelaySeconds: %w", err)
		}
		proofMaturityDelay := time.Duration(proofMaturityDelaySeconds.Int64() * int64(time.Second))

		finalityDelaySeconds, err := optimismPortal.DisputeGameFinalityDelaySeconds(&bind.CallOpts{})
		if err != nil {
			return fmt.Errorf("could not call OptimismPortal.DisputeGameFinalityDelaySeconds: %w", err)
		}
		finalityDelay := time.Duration(finalityDelaySeconds.Int64() * int64(time.Second))

		proofMaturityTime := provenTimestamp.Add(proofMaturityDelay)
		finalityDelayTime := disputeGameResolvedAtTime.Add(finalityDelay)
		untilProofMaturityTime := time.Until(proofMaturityTime)
		untilFinalityDelayTime := time.Until(finalityDelayTime)

		if untilProofMaturityTime > 0 || untilFinalityDelayTime > 0 {
			log.Info("either the proof has not matured long enough or the finality period has not passed, exiting...",
				"proofMaturityTime", proofMaturityTime,
				"finalityDelayTime", finalityDelayTime,
				"until proofMaturityTime", untilProofMaturityTime,
				"until finalityDelayTime", untilFinalityDelayTime,
			)
			return nil
		} else {
			log.Info("the withdrawal proof has matured long enough and the finality period has passed, continuing...",
				"proofMaturityTime", proofMaturityTime,
				"finalityDelayTime", finalityDelayTime,
				"since proofMaturityTime", -untilProofMaturityTime,
				"since finalityDelayTime", -untilFinalityDelayTime,
			)
		}

		withdrawalFinalized, err := optimismPortal.FinalizedWithdrawals(&bind.CallOpts{}, messagePassedEvent.WithdrawalHash)
		if err != nil {
			return fmt.Errorf("could not fetch OptimismPortal.FinalizedWithdrawals: %w", err)
		}
		if withdrawalFinalized {
			log.Info("withdrawal proof has already been finalized, exiting...", "withdrawal hash", common.Bytes2Hex(messagePassedEvent.WithdrawalHash[:]))
			return nil
		} else {
			log.Info("withdrawal proof has not been finalized, continuing...")
		}

		log.Info("calling OptimismPortal.CheckWithdrawal to validate that withdrawal can be finalized")
		err = optimismPortal.CheckWithdrawal(&bind.CallOpts{}, messagePassedEvent.WithdrawalHash, account)
		if err != nil {
			log.Info("Optimism.CheckWithdrawal failed, exiting...", "error", err)
			return fmt.Errorf("call to OptimismPortal.CheckWithdrawal failed: %w", err)
		} else {
			log.Info("call to Optimism.CheckWithdrawal succeeded, proceeding with finalizeWithdrawal transaction...")
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

		log.Info("calling OptimismPortal.FinalizeWithdrawalTransaction")
		tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
			return optimismPortal.FinalizeWithdrawalTransaction(
				opts,
				bindingspreview.TypesWithdrawalTransaction{
					Nonce:    params.Nonce,
					Sender:   params.Sender,
					Target:   params.Target,
					Value:    params.Value,
					GasLimit: params.GasLimit,
					Data:     params.Data,
				},
			)
		})
		if err != nil {
			return fmt.Errorf("failed to send OptimismPortal.FinalizeWithdrawalTransaction(): %w", err)
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
		log.Info("successfully executed OptimismPortal.FinalizedWithdrawalTransaction(), exiting...", "tx", receipt.TxHash.Hex())

		postBalance, err := l1Client.BalanceAt(ctx, account, nil)
		if err != nil {
			return fmt.Errorf("could not fetch balance: %w", err)
		}

		log.Info("successfully finalized withdrawal transaction", "initTx", withdrawalTxHash.Hex(), "amount", postBalance.Uint64()-preBalance.Uint64())

		return nil
	},
}
