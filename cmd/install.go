package cmd

import (
	"fmt"
	"os"
	"synco/internal/autostart"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Register as service on boot",
	RunE: func(cmd *cobra.Command, args []string) error {
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}

		as := autostart.New()
		if err := as.Install(execPath); err != nil {
			return err
		}

		fmt.Println("synco daemon registered for autostart")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installCmd)
}
