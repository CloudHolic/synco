package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"synco/internal/autostart"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Register synco daemon for autostart on login",
	RunE: func(cmd *cobra.Command, args []string) error {
		execPath, err := os.Executable()
		if err != nil {
			return err
		}

		as := autostart.New()
		if err := as.Install(execPath, []string{"daemon", "start"}); err != nil {
			return fmt.Errorf("failed to install autostart: %w", err)
		}

		fmt.Println("synco will now start automatically on login")

		if !isDaemonRunning() {
			fmt.Println("starting daemon now...")
			c := exec.Command(execPath, "daemon", "start")
			if err := c.Start(); err != nil {
				return fmt.Errorf("failed to start daemon: %w", err)
			}

			fmt.Println("daemon started successfully")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
