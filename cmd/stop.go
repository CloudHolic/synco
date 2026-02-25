package cmd

import (
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Post(daemonURL("/stop"), "application/json", nil)
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		fmt.Println("stopped")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
