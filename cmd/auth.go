package cmd

import (
	"fmt"
	"synco/internal/auth"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication for cloud services",
}

var authGDriveCmd = &cobra.Command{
	Use:   "gdrive",
	Short: "Authenticate with Google Drive",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.Authorize(); err != nil {
			return err
		}

		fmt.Println("Authenticated with Google Drive")
		return nil
	},
}

func init() {
	authCmd.AddCommand(authGDriveCmd)
	rootCmd.AddCommand(authCmd)
}
