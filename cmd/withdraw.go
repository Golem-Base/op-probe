package cmd

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/Golem-Base/op-probe/internal"

	e2eBindings "github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	opNodeBindings "github.com/ethereum-optimism/optimism/op-node/bindings"
	opNodePreviewBindings "github.com/ethereum-optimism/optimism/op-node/bindings/preview"
	"github.com/ethereum-optimism/optimism/op-node/withdrawals"
	"github.com/ethereum-optimism/optimism/op-service/predeploys"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

// https://github.com/ethereum-optimism/optimism/blob/79cfece0e55f363c06e98e1019eb952cae18c858/packages/contracts-bedrock/src/L2/L2ToL1MessagePasser.sol#L21
const RECEIVE_DEFAULT_GAS_LIMIT uint64 = 100_000

const PROPOSER = "0x7fAE81D16E6894AAF8fD25a01eee3359Ce95b538"

var ProposerAddress = common.HexToAddress(PROPOSER)

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

var list = &cli.Command{
	Name:  "list",
	Usage: "Lists all ongoing withdrawals and their statuses",
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
			Name:     "optimism-portal2-address",
			Usage:    "Contract address for OptimismPortal2 (* or proxy)",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()

		l1RpcUrl := c.String("l1-rpc-url")

		l1Client, err := ethclient.Dial(l1RpcUrl)
		if err != nil {
			return fmt.Errorf("could not dial l1 rpc at %s: %w", l1RpcUrl, err)
		}

		l2RpcUrl := c.String("l2-rpc-url")

		l2Client, err := ethclient.Dial(l2RpcUrl)
		if err != nil {
			return fmt.Errorf("could not dial l2 rpc at %s: %w", l2RpcUrl, err)
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

		optimismPortal2Address, err := internal.SafeParseAddress(c.String("optimism-portal2-address"))
		if err != nil {
			return fmt.Errorf("could not parse DisputeGameFactory address: %w", err)
		}
		optimismPortal2, err := opNodePreviewBindings.NewOptimismPortal2(optimismPortal2Address, l1Client)
		if err != nil {
			return fmt.Errorf("could not instantiate OptimismPortal2 contract: %w", err)
		}

		l2ToL1MessagePasserFilterer, err := e2eBindings.NewL2ToL1MessagePasserFilterer(predeploys.L2ToL1MessagePasserAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not instantiate L2ToL2MessagePasser filterer")
		}

		game, err := withdrawals.FindLatestGame(ctx, &disputeGameFactory.DisputeGameFactoryCaller, &optimismPortal2.OptimismPortal2Caller)
		if err != nil {
			return fmt.Errorf("failed to find latest game: %w", err)
		}

		gameL2BlockNumber := new(big.Int).SetBytes(game.ExtraData[0:32])

		log.Info("Found latest game", "game", game.Index, "l2Block", gameL2BlockNumber, "timestamp", time.Unix(int64(game.Timestamp), 0))

		// TODO This is possibly naïve behaviour and might need better filtering to identify withdrawals specifically
		iterator, err := l2ToL1MessagePasserFilterer.FilterMessagePassed(&bind.FilterOpts{Context: ctx, Start: 0, End: nil}, nil, []common.Address{account}, []common.Address{account})

		for iterator.Next() {
			// If the event exists, then the withdrawal is at minimum
			status := Initialized

			event := iterator.Event

			// Get the block to retrieve its timestamp
			block, err := l2Client.BlockByNumber(ctx, big.NewInt(int64(event.Raw.BlockNumber)))
			if err != nil {
				return fmt.Errorf("failed to get block %d: %w", event.Raw.BlockNumber, err)
			}

			valueInEth := new(big.Float).Quo(
				new(big.Float).SetInt(event.Value),
				new(big.Float).SetInt(big.NewInt(1e18)),
			)

			// If the latest deployed game is greater than the L2 block of the withdrawal transaction then
			// it's at least provable
			if gameL2BlockNumber.Uint64() > block.Number().Uint64() {
				status = Provable
			}
			if status == Provable {
				// Check only provable transactions as proven
				proven, err := optimismPortal2.ProvenWithdrawals(&bind.CallOpts{}, event.WithdrawalHash, ProposerAddress)
				if err != nil {
					return fmt.Errorf("could not fetch proven withdrawal: %w", err)
				}

				if proven.DisputeGameProxy != common.BytesToAddress([]byte{0}) {
					status = Proven
				}
			}

			// Extract timestamp from the block
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

	// Extract the nonce part (lower 240 bits)
	nonceValue := new(big.Int).And(nonce, mask)

	return nonceValue
}

var initialize = &cli.Command{
	Name:  "init",
	Usage: "Initialize a new withdrawal",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "private-key",
			Usage:    "Private key of address to send test transaction from",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "l2-rpc-url",
			Usage:    "Url for L2 execution client",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "value",
			Usage:    "Amount to withdraw from L2 to L1 (wei)",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()

		l2RpcUrl := c.String("l2-rpc-url")

		l2Client, err := ethclient.Dial(l2RpcUrl)
		if err != nil {
			return fmt.Errorf("could not dial l2 rpc at %s: %w", l2RpcUrl, err)
		}

		value, err := internal.ParseUint256BigInt(c.String("value"))
		if err != nil {
			return err
		}

		privateKey, err := crypto.HexToECDSA(c.String("private-key"))
		if err != nil {
			return fmt.Errorf("failed to parse private-key: %w", err)
		}

		account := crypto.PubkeyToAddress(privateKey.PublicKey)

		balance, err := l2Client.BalanceAt(ctx, account, nil)
		if err != nil {
			return fmt.Errorf("could not get ETH balance for %s: %w", account, err)
		}
		if balance.Cmp(value) == -1 {
			return fmt.Errorf("account %s does not have enough balance", account)
		}

		log.Info("initiating withdrawal", "account", account, "balance", balance, "amount", value)

		l2ChainId, err := l2Client.ChainID(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch l2 network id: %w", err)
		}

		code, err := l2Client.CodeAt(context.Background(), predeploys.L2ToL1MessagePasserAddr, nil) // nil means latest block
		if err != nil {
			return fmt.Errorf("failed to get code at L2ToL1MessagePasser predeploy address (%s): %w", predeploys.L2ToL1MessagePasserAddr, err)
		}
		if len(code) == 0 {
			return fmt.Errorf("L2ToL1MessagePasser (%s) is not deployed", predeploys.L2ToL1MessagePasserAddr)
		}

		l2ToL1MessagePasser, err := e2eBindings.NewL2ToL1MessagePasser(predeploys.L2ToL1MessagePasserAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not not instantiate L2ToL1MessagePasser contract: %w", err)
		}

		opts, err := bind.NewKeyedTransactorWithChainID(privateKey, l2ChainId)
		if err != nil {
			return fmt.Errorf("could not setup transactor: %w", err)
		}
		opts.Value = value

		tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
			return l2ToL1MessagePasser.InitiateWithdrawal(opts, opts.From, big.NewInt(int64(RECEIVE_DEFAULT_GAS_LIMIT)), []byte{})
		})
		if err != nil {
			return fmt.Errorf("could not construct transaction to initiate withdrawal: %w", err)
		}

		log.Info("sent withdrawal initialization transaction", "tx", tx.Hash().Hex())

		receipt, err := wait.ForReceiptOK(ctx, l2Client, tx.Hash())
		if err != nil {
			if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
				log.Error("bridgeETH transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
				return fmt.Errorf("failure in bridge transaction execution: %w", err)
			} else {
				return fmt.Errorf("failed to get bridge transaction receipt: %w", err)
			}
		}

		log.Info("successfully initialized withdrawal", "receipt", receipt)

		return nil
	},
}

// var prove = &cli.Command{
// 	Name:  "prove",
// 	Usage: "Prove a withdrawal by specifying a specific transaction hash",
// 	Action: func(cCtx *cli.Context) error {
// 		return nil
// 	},
// }

// var finalize = &cli.Command{
// 	Name:  "finalize",
// 	Usage: "Finalize a withdrawal",
// 	Action: func(cCtx *cli.Context) error {
// 		return nil
// 	},
// }

var WithdrawCommand = &cli.Command{
	Name:  "withdraw",
	Usage: "Performs optimism withdrawal operations",
	Subcommands: []*cli.Command{
		list,
		initialize,
		// prove,
		// finalize,
	},
	Action: func(cCtx *cli.Context) error {
		fmt.Println("Withdraw command requires a subcommand: list, init, prove, or finalize")
		cli.ShowSubcommandHelp(cCtx)
		return nil
	},
}

// var Withdraw = &cli.Command{
// 	Name:  "withdraw",
// 	Usage: "Performs withdraw actions on Optimism Chains",

// 	// Flags: []cli.Flag{
// 	// 	&cli.StringFlag{
// 	// 		Name:     "private-key",
// 	// 		Usage:    "Private key of address to send test transaction from",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "l1-rpc-url",
// 	// 		Usage:    "Url for L1 execution client",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "l2-rpc-url",
// 	// 		Usage:    "Url for L2 execution client",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "optimism-portal-address",
// 	// 		Usage:    "Contract address for the OptimismPortal (* or proxy)",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "l1-standard-bridge-address",
// 	// 		Usage:    "Contract address for the L1StandardBridge (* or proxy)",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "l2-standard-bridge-address",
// 	// 		Usage:    "Contract address for L2StandardBridge (* or proxy)",
// 	// 		Required: true,
// 	// 	},
// 	// 	&cli.StringFlag{
// 	// 		Name:     "value",
// 	// 		Usage:    "Amount to deposit from L1 to L2 (wei)",
// 	// 		Required: true,
// 	// 	},
// 	// },
// 	// Action: func(c *cli.Context) error {
// 	// 	return nil
// 	// },
// }
