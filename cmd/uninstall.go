package cmd

import (
	"fmt"
	"synco/internal/autostart"

	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unregister services",
	RunE: func(cmd *cobra.Command, args []string) error {
		as := autostart.New()
		if err := as.Uninstall(); err != nil {
			return err
		}

		fmt.Println("synco daemon autostart removed")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
