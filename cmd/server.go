package cmd

import (
	"dtm/web"

	"github.com/spf13/cobra"
)

func serverCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web server",
		Long:  `This command starts the web server for the application.`,
		Run: func(cmd *cobra.Command, args []string) {
			// Start the web server
			web.Serve()
		},
	}

	return cmd
}
