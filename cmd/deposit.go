package cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Golem-Base/op-probe/internal"

	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/receipts"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var DepositCommand = &cli.Command{
	Name:  "deposit",
	Usage: "Deposits ETH from L1 to L2",

	// Example flags
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
			Name:     "optimism-portal-address",
			Usage:    "Contract address for the OptimismPortal (* or proxy)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "l1-standard-bridge-address",
			Usage:    "Contract address for the L1StandardBridge (* or proxy)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "amount",
			Usage:    "Amount to deposit from L1 to L2 (wei)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "recipient",
			Usage:    "Address to receive amount",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()

		amount, err := internal.ParseUint256BigInt(c.String("amount"))
		if err != nil {
			return err
		}

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

		sender := crypto.PubkeyToAddress(privateKey.PublicKey)

		recipient, err := internal.SafeParseAddress(c.String("recipient"))
		if err != nil {
			return fmt.Errorf("could not parse recipient address: %w", err)
		}

		senderPreBalance, err := l1Client.BalanceAt(ctx, sender, nil)
		recipientPreBalance, err := l2Client.BalanceAt(ctx, recipient, nil)

		contracts, err := internal.NewDepositContracts(
			ctx,
			l1Client,
			l2Client,
			c.String("optimism-portal-address"),
			c.String("l1-standard-bridge-address"),
		)
		if err != nil {
			return fmt.Errorf("could not instantiate deposit contracts: %w", err)
		}

		opts, err := bind.NewKeyedTransactorWithChainID(privateKey, l1ChainId)
		if err != nil {
			return fmt.Errorf("could not setup transactor: %w", err)
		}
		opts.Value = amount

		log.Info("executing l1StandardBridge.bridgeETH transaction")

		tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
			return contracts.L1StandardBridge.DepositETHTo(opts, recipient, internal.RECEIVE_DEFAULT_GAS_LIMIT, []byte{})
		})
		if err != nil {
			return fmt.Errorf("could not construct calldata for DepositETH: %w", err)
		}

		log.Info("sent transaction, waiting for confirmation", "tx", tx.Hash().Hex())

		receipt, err := wait.ForReceiptOK(ctx, l1Client, tx.Hash())
		if err != nil {
			if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
				log.Error("bridgeETH transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
				return fmt.Errorf("failure in bridge transaction execution: %w", err)
			} else {
				return fmt.Errorf("failed to get bridge transaction receipt: %w", err)
			}
		}

		log.Info("transaction has been mined successfully", "receipt", receipt)

		transactionDepositedEvent, err := receipts.FindLog(receipt.Logs, contracts.OptimismPortal.ParseTransactionDeposited)
		if err != nil {
			return fmt.Errorf("could not parse OptimismPortal.TransactionDeposited event from the receipt logs: %w", err)
		}

		log.Info("found TransactionDeposited event in receiptLog", "event", transactionDepositedEvent.Raw)

		// The L2 special deposit transaction can be dervied from the TransactionDeposited logs
		depositTx, err := derive.UnmarshalDepositLogEvent(&transactionDepositedEvent.Raw)
		if err != nil {
			return fmt.Errorf("encountered error deriving the deposit transaction type from the OptimismPortal.TransactionDeposited event: %w", err)
		}

		log.Info("successfully derived the L2 deposit transaction", "depositTx", depositTx)

		depositTxHash := types.NewTx(depositTx).Hash()

		log.Info("waiting for deposit transaction reciept on L2", "tx", depositTxHash)

		receipt, err = wait.ForReceiptOK(ctx, l2Client, depositTxHash)
		if err != nil {
			if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
				log.Error("deposit transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
				return fmt.Errorf("failure in deposit execution: %w", err)
			} else {
				return fmt.Errorf("found error waiting for deposit receipt: %w", err)
			}
		}

		log.Info("deposit transaction successfully propogated to L2", "receipt", receipt)

		senderPostBalance, err := l1Client.BalanceAt(ctx, sender, nil)
		recipientPostBalance, err := l2Client.BalanceAt(ctx, recipient, nil)

		senderDiff := new(big.Int).Sub(senderPreBalance, senderPostBalance)
		recipientDiff := new(big.Int).Sub(recipientPostBalance, recipientPreBalance)
		gasSpent := new(big.Int).Sub(senderDiff, recipientDiff)

		log.Info(
			"Balance differentials",
			"recipient L2 balance (+)", internal.FormatWei(recipientDiff),
			"sender L1 balance (-)", internal.FormatWei(senderDiff),
			"gas", internal.FormatWei(gasSpent),
		)

		return nil
	},
}
