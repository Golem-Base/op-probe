package main

import (
	"os"

	"github.com/Golem-Base/op-devnet/probe/cmd"

	"github.com/ethereum/go-ethereum/log"
	"github.com/urfave/cli/v2"
)

func main() {
	log.SetDefault(log.NewLogger(log.JSONHandlerWithLevel(os.Stdout, log.LevelInfo)))
	app := &cli.App{
		Name:  "probe",
		Usage: "Helper utilities for devnet",
		Commands: []*cli.Command{
			cmd.SendOnReadyCommand,
			cmd.BridgeEthAndFinalizeCommand,
			cmd.Withdraw,
		},
	}

	// Run the CLI
	err := app.Run(os.Args)
	if err != nil {
		log.Crit("", app.Name, err)
	}
}
