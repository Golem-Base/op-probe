package withdraw_cmd

import (
	"context"
	"fmt"
	"math/big"

	"github.com/Golem-Base/op-probe/internal"
	e2eBindings "github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	"github.com/ethereum-optimism/optimism/op-service/predeploys"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

const RECEIVE_DEFAULT_GAS_LIMIT uint64 = 100_000

var InitCommand = &cli.Command{
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
			Name:     "amount",
			Usage:    "Amount to withdraw from L2 to L1 (wei)",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		ctx := context.Background()

		l2RpcUrl := c.String("l2-rpc-url")
		l2Client, _, err := internal.ConnectClient(ctx, l2RpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", l2RpcUrl, err)
		}

		amount, err := internal.ParseUint256BigInt(c.String("amount"))
		if err != nil {
			return err
		}

		privateKey, err := crypto.HexToECDSA(c.String("private-key"))
		if err != nil {
			return fmt.Errorf("failed to parse private-key: %w", err)
		}

		account := crypto.PubkeyToAddress(privateKey.PublicKey)

		log.Info("initiating withdrawal", "account", account, "amount", amount)

		l2ChainId, err := l2Client.ChainID(ctx)
		if err != nil {
			return fmt.Errorf("could not fetch l2 network id: %w", err)
		}

		l2ToL1MessagePasser, err := e2eBindings.NewL2ToL1MessagePasser(predeploys.L2ToL1MessagePasserAddr, l2Client)
		if err != nil {
			return fmt.Errorf("could not not instantiate L2ToL1MessagePasser contract: %w", err)
		}

		opts, err := bind.NewKeyedTransactorWithChainID(privateKey, l2ChainId)
		if err != nil {
			return fmt.Errorf("could not setup transactor: %w", err)
		}
		opts.Value = amount

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
