package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"synco/logger"
	"synco/server"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	serveTarget string
	serveAddr   string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start a file-sync server to receive files",
	RunE: func(cmd *cobra.Command, args []string) error {
		defer logger.Sync()

		if serveTarget == "" {
			return fmt.Errorf("--target is required")
		}

		srv, err := server.New(serveTarget, serveAddr, cfg.ConflictStrategy)
		if err != nil {
			return err
		}

		if err := srv.Start(); err != nil {
			return err
		}

		logger.Log.Info("synco server ready",
			zap.String("addr", serveAddr),
			zap.String("target", serveTarget))

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		srv.Stop()
		return nil
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveTarget, "target", "", "target directory")
	serveCmd.Flags().StringVar(&serveAddr, "addr", ":8080", "listen address")
	rootCmd.AddCommand(serveCmd)
}
