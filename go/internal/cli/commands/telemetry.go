package commands

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/wendylabsinc/wendy/proto/gen/agentpb"
)

func newTelemetryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Stream telemetry data from the target device",
	}

	cmd.AddCommand(
		newTelemetryStreamCmd(),
	)

	return cmd
}

func newTelemetryStreamCmd() *cobra.Command {
	var appName string
	var serviceName string

	cmd := &cobra.Command{
		Use:   "stream",
		Short: "Stream telemetry data as JSONL",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			conn, err := connectToAgent(ctx)
			if err != nil {
				return err
			}
			defer conn.Close()

			streamReq := &agentpb.StreamLogsRequest{}
			if appName != "" {
				streamReq.AppName = &appName
			}
			if serviceName != "" {
				streamReq.ServiceName = &serviceName
			}
			stream, err := conn.TelemetryService.StreamLogs(ctx, streamReq)
			if err != nil {
				return fmt.Errorf("starting telemetry stream: %w", err)
			}

			for {
				resp, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("receiving telemetry: %w", err)
				}

				data, err := json.Marshal(resp)
				if err != nil {
					continue
				}
				fmt.Println(string(data))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appName, "app", "", "Filter by application name")
	cmd.Flags().StringVar(&serviceName, "service", "", "Filter by service name")

	return cmd
}
