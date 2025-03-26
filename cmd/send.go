package cmd

import (
	"context"
	"fmt"

	"github.com/Golem-Base/op-probe/internal"

	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-service/txmgr"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

var SendCommand = &cli.Command{
	Name:  "send",
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
			Name:     "amount",
			Usage:    "Amount to send in wei",
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

		sender := crypto.PubkeyToAddress(privateKey.PublicKey)

		rpcUrl := c.String("rpc-url")
		client, _, err := internal.ConnectClient(ctx, rpcUrl)
		if err != nil {
			return fmt.Errorf("could not connect to client at %s: %w", rpcUrl, err)
		}

		recipient, err := internal.SafeParseAddress(c.String("recipient"))
		if err != nil {
			return fmt.Errorf("could not parse recipient address: %w", err)
		}

		log.Info("sending transaction", "amount", amount, "sender", sender, "recipient", recipient)

		candidate := txmgr.TxCandidate{
			To:       &internal.ZeroAddress,
			GasLimit: 21000,
			Value:    amount,
		}
		_, receipt, err := transactions.SendTx(ctx, client, candidate, privateKey)

		log.Info("successfully sent transaction", "tx", receipt.TxHash.Hex())

		return nil
	},
}
