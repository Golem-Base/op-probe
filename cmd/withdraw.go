package cmd

import (
	"fmt"
	withdraw_cmd "github.com/Golem-Base/op-probe/cmd/withdraw"
	"github.com/urfave/cli/v2"
)

var WithdrawCommand = &cli.Command{
	Name:  "withdraw",
	Usage: "Performs optimism withdrawal operations",
	Subcommands: []*cli.Command{
		withdraw_cmd.ListCommand,
		withdraw_cmd.InitCommand,
		withdraw_cmd.ProveCommand,
		withdraw_cmd.FinalizeCommand,
	},
	Action: func(cCtx *cli.Context) error {
		fmt.Println("Withdraw command requires a subcommand: list, init, prove, or finalize")
		cli.ShowSubcommandHelp(cCtx)
		return nil
	},
}
