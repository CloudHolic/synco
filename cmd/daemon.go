package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"synco/internal/daemon"
	"synco/internal/logger"
	"synco/internal/repository"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the synco daemon process",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon (invoked by autostart or 'job add --foreground')",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemonInProcess("", "")
	},
}

func runDaemonInProcess(extraSrc, extraDst string) error {
	defer logger.Sync()

	jobRepo := repository.NewJobRepository()
	jobs, err := jobRepo.GetAll()
	if err != nil {
		return err
	}

	manager, err := daemon.NewJobManager(cfg)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		if err := manager.StartJob(job); err != nil {
			logger.Log.Warn("failed to start stored job",
				zap.Uint("id", job.ID),
				zap.Error(err))
		}
	}

	if extraSrc != "" && extraDst != "" {
		srcType := endpointType(extraSrc)
		dstType := endpointType(extraDst)
		job, err := jobRepo.Add(extraSrc, srcType, extraDst, dstType)
		if err != nil {
			return fmt.Errorf("failed to add job: %w", err)
		}

		if err := manager.StartJob(job); err != nil {
			return fmt.Errorf("failed to start job: %w", err)
		}

		fmt.Printf("watching %s → %s  (Ctrl+C to stop)\n", extraSrc, extraDst)
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
	daemonCmd.AddCommand(daemonStartCmd)
	rootCmd.AddCommand(daemonCmd)
}
