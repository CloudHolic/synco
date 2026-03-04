package cmd

import (
	"fmt"
	"io"
	"net/http"
	"synco/internal/autostart"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		as := autostart.New()

		if as.IsInstalled() {
			if err := as.Stop(); err != nil {
				return fmt.Errorf("failed to stop daemon: %w", err)
			}

			_ = httpStop()
			fmt.Println("daemon stopped")
			fmt.Println("note: autostart has been disabled. run 'synco install' to re-enable")
			return nil
		}

		if err := httpStop(); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}

		fmt.Println("daemon stopped")
		return nil
	},
}

func httpStop() error {
	resp, err := apiPost("/stop", "", nil)
	if err != nil {
		return err
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
