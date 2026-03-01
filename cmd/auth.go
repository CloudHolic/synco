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

var authDropboxCmd = &cobra.Command{
	Use:   "dropbox",
	Short: "Authenticate with Dropbox",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.Dropbox.Authorize(); err != nil {
			return err
		}

		fmt.Println("Authenticated with Dropbox")
		return nil
	},
}

var authGDriveCmd = &cobra.Command{
	Use:   "gdrive",
	Short: "Authenticate with Google Drive",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := auth.GDrive.Authorize(); err != nil {
			return err
		}

		fmt.Println("Authenticated with Google Drive")
		return nil
	},
}

func init() {
	authCmd.AddCommand(authDropboxCmd, authGDriveCmd)
	rootCmd.AddCommand(authCmd)
}
