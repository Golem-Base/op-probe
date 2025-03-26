package cmd

import (
	"context"
	"fmt"

	"github.com/Golem-Base/op-devnet/probe/bridge"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/urfave/cli/v2"
)

var BridgeEthAndFinalizeCommand = &cli.Command{
	Name:  "bridgeEthAndFinalize",
	Usage: "Waits for the L2 to produce blocks and attempts to bridge ETH from L1 to L2",

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
			Name:     "l2-standard-bridge-address",
			Usage:    "Contract address for L2StandardBridge (* or proxy)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "value",
			Usage:    "Amount to deposit from L1 to L2 (wei)",
			Required: true,
		},
	},
	Action: func(c *cli.Context) error {
		value, err := bridge.ParseUint256BigInt(c.String("value"))
		if err != nil {
			return err
		}

		privateKey, err := crypto.HexToECDSA(c.String("private-key"))
		if err != nil {
			return fmt.Errorf("failed to parse private-key: %w", err)
		}

		ctx := context.Background()

		bridger, err := bridge.NewBridger(
			ctx,
			c.String("l1-rpc-url"),
			c.String("l2-rpc-url"),
			c.String("optimism-portal-address"),
			c.String("l1-standard-bridge-address"),
			c.String("l2-standard-bridge-address"),
		)
		if err != nil {
			return fmt.Errorf("failed to create new bridger: %w", err)
		}

		if err = bridger.BridgeETHFromL1ToL2(ctx, privateKey, value); err != nil {
			return fmt.Errorf("failed to bridge ETH from L1 to L2: %w", err)
		}

		return nil
	},
}
