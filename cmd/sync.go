package cmd

import (
	"fmt"
	"synco/logger"
	"synco/repository"
	"synco/syncer"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var syncCmd = &cobra.Command{
	Use:   "sync [source] [destination]",
	Short: "Sync all files once",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		defer logger.Sync()
		src, dst := args[0], args[1]

		s, err := syncer.NewLocalSyncer(src, dst)
		if err != nil {
			return err
		}

		logger.Log.Info("starting full sync",
			zap.String("src", src),
			zap.String("dst", dst))

		results, err := s.FullSync()
		if err != nil {
			return err
		}

		repo := repository.NewHistoryRepository()

		var synced, failed int
		for _, r := range results {
			if err := repo.Save(r); err != nil {
				logger.Log.Warn("failed to save history",
					zap.Error(err))
			}

			if r.Err != nil {
				failed++
			} else {
				synced++
			}
		}

		fmt.Printf("done: %d synced, %d failed\n", synced, failed)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(syncCmd)
}
