package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"synco/internal/daemon"
	"time"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "View daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(daemonURL("/status"))
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		var result struct {
			Jobs []daemon.JobSnapshot `json:"jobs"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to decode status response: %w", err)
		}

		if len(result.Jobs) == 0 {
			fmt.Println("no active jobs")
			return nil
		}

		fmt.Printf("%-6s %-10s %-30s %-30s %-8s %-8s %s\n",
			"JOB", "STATUS", "SRC", "DST", "SYNCED", "FAILED", "LAST SYNC")

		for _, snap := range result.Jobs {
			lastSync := "-"
			if snap.LastSync != nil {
				lastSync = snap.LastSync.Format("2006-01-02 15:04:05")
			}

			uptime := time.Since(snap.StartedAt).Round(time.Second)
			fmt.Printf("%-6d %-10s %-30s %-30s %-8d %-8d %s\n",
				snap.JobID, snap.Status, snap.Src, snap.Dst, snap.Synced, snap.Failed, lastSync)
			fmt.Printf("       uptime: %s\n", uptime)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
