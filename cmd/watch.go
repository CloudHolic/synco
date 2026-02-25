package cmd

import (
	"context"
	"os"
	"os/signal"
	"synco/daemon"
	"synco/logger"
	"synco/repository"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Start the daemon using all the stored jobs",
	RunE:  runDaemon,
}

func runDaemon(cmd *cobra.Command, args []string) error {
	defer logger.Sync()

	jobRepo := repository.NewJobRepository()
	jobs, err := jobRepo.GetAll()
	if err != nil {
		return err
	}

	manager := daemon.NewJobManager(cfg)

	for _, job := range jobs {
		if err := manager.StartJob(job); err != nil {
			logger.Log.Warn("failed to start job",
				zap.Uint("id", job.ID),
				zap.Error(err))
		}
	}

	if len(jobs) == 0 {
		logger.Log.Info("no jobs configured, use 'synco job add <src> <dst>' to add one")
	}

	srv := daemon.NewServer(manager, cfg.DaemonPort)
	srv.Start()

	logger.Log.Info("synco daemon started",
		zap.Int("jobs", len(jobs)),
		zap.Int("port", cfg.DaemonPort))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		logger.Log.Info("shutting down",
			zap.String("signal", sig.String()))
	case <-srv.StopCh():
		logger.Log.Info("stop requested via API")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Stop(ctx)
}

func init() {
	rootCmd.AddCommand(watchCmd)
}
