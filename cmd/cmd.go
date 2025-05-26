package cmd

import (
	"github.com/spf13/cobra"
)

var RootCmd = &cobra.Command{
	Use:   "dtm",
	Short: "share payment after trip",
	Long:  `this is a tool to share payment after trip, it will help you to split the cost among friends`,
}

func init() {
	RootCmd.AddCommand(shareCmd())
	RootCmd.AddCommand(serverCommand())
}
