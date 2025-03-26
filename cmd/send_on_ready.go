package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/Golem-Base/op-devnet/probe/bridge"

	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var SendOnReadyCommand = &cli.Command{
	Name:  "sendOnReady",
	Usage: "Waits for the client to produce blocks and attempts to transfer ETH",

	// Example flags
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "rpc-url",
			Usage:    "Url for exection client",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "private-key",
			Usage:    "Private key of address to send test transaction from",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "value",
			Usage:    "amount to send",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		value, err := bridge.ParseUint256BigInt(c.String("value"))
		if err != nil {
			return err
		}

		ctx := context.Background()

		client, err := ethclient.Dial(c.String("rpc-url"))
		if err != nil {
			return fmt.Errorf("could not dial rpc at %s: %w", c.String("rpc-url"), err)
		}
		privateKey, err := crypto.HexToECDSA(c.String("private-key"))
		if err != nil {
			return fmt.Errorf("failed to parse private-key: %w", err)
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := bridge.WaitForChainsStart(timeoutCtx, []*ethclient.Client{client}); err != nil {
			return fmt.Errorf("Chain did not start: %w", err)
		}

		zeroAddress := common.HexToAddress(bridge.ZeroAddress) // Get value
		candidate := txmgr.TxCandidate{
			To:       &zeroAddress,
			GasLimit: 21000,
			Value:    value,
		}
		_, receipt, err := transactions.SendTx(ctx, client, candidate, privateKey)

		log.Info("Successfully sent transaction", "tx", receipt.TxHash.Hex())

		return nil
	},
}
