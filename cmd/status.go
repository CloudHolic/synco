package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
			Daemon struct {
				PID       int       `json:"pid"`
				StartedAt time.Time `json:"started_at"`
			} `json:"daemon"`
			Jobs []struct {
				JobID     uint       `json:"job_id"`
				Src       string     `json:"src"`
				Dst       string     `json:"dst"`
				Status    string     `json:"status"`
				Synced    int        `json:"synced"`
				Failed    int        `json:"failed"`
				LastSync  *time.Time `json:"last_sync"`
				StartedAt time.Time  `json:"started_at"`
			} `json:"jobs"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}

		uptime := time.Since(result.Daemon.StartedAt).Round(time.Second)
		fmt.Printf("● synco daemon  running  (pid %d, uptime %s)\n\n",
			result.Daemon.PID, formatDuration(uptime))

		if len(result.Jobs) == 0 {
			fmt.Println("  no active jobs — use 'synco job add <src> <dst>'")
			return nil
		}

		fmt.Printf("%-4s %-8s %-28s %-28s %-8s %-8s %s\n",
			"ID", "STATUS", "SRC", "DST", "SYNCED", "FAILED", "LAST SYNC")

		for _, j := range result.Jobs {
			lastSync := "-"
			if j.LastSync != nil {
				lastSync = formatAgo(*j.LastSync)
			}

			fmt.Printf("%-4d %-8s %-28s %-28s %-8d %-8d %s\n",
				j.JobID, j.Status, truncate(j.Src, 28), truncate(j.Dst, 28), j.Synced, j.Failed, lastSync)
			fmt.Printf("       uptime: %s\n", uptime)
		}

		return nil
	},
}

func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}

	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}

	return fmt.Sprintf("%ds", s)
}

func formatAgo(t time.Time) string {
	d := time.Since(t).Round(time.Second)

	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}

	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}

	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return "…" + s[len(s)-(max-1):]
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
