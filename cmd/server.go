package cmd

import (
	"dtm/mq/mq"
	"dtm/web"

	"github.com/spf13/cobra"
)

func serverCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web server",
		Long:  `This command starts the web server for the application.`,
		Run: func(cmd *cobra.Command, args []string) {
			isDev := cmd.Flags().Lookup("dev").Value.String() == "true"
			port := cmd.Flags().Lookup("port").Value.String()
			mqMode := cmd.Flags().Lookup("mq").Value.String()

			// Start the web server
			web.Serve(web.WebServiceConfig{
				IsDev:  isDev,
				Port:   port,
				MqMode: mq.MqMode(mqMode),
			})
		},
	}

	cmd.Flags().Bool("dev", true, "Run in development mode")
	cmd.Flags().String("port", "8080", "Port to run the web server on")
	cmd.Flags().String("mq", "gochan", "Message queue mode (gochan, rabbitmq, gcppubsub)")

	return cmd
}
