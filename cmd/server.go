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
			web.Serve(web.WebServiceConfig{
				IsDev:    cmd.Flags().Lookup("dev").Value.String() == "true",
				Port:     cmd.Flags().Lookup("port").Value.String(),
			})
		},
	}

	cmd.Flags().Bool("dev", true, "Run in development mode")
	cmd.Flags().String("port", "8080", "Port to run the web server on")

	return cmd
}
