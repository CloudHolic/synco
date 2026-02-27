package cmd

import (
	"fmt"
	"os"
	"synco/internal/config"
	"synco/internal/db"
	"synco/internal/logger"

	"github.com/spf13/cobra"
)

var (
	cfg   *config.Config
	debug bool
)

var rootCmd = &cobra.Command{
	Use:   "synco",
	Short: "A simple file sync tool",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.Name() == "completion" {
			return nil
		}

		logger.Init(debug)

		var err error
		cfg, err = config.Load()
		if err != nil {
			return err
		}

		clientCmds := map[string]bool{
			"status": true, "pause": true,
			"resume": true, "stop": true, "history": true,
		}
		if !clientCmds[cmd.Name()] {
			if err := db.Init(cfg.DBPath); err != nil {
				return err
			}
		}

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func daemonURL(path string) string {
	return fmt.Sprintf("http://localhost:%d%s", cfg.DaemonPort, path)
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")
}
