package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"synco/internal/model"
	"synco/internal/syncer/tcp"

	"github.com/spf13/cobra"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage jobs",
}

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all jobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Get(daemonURL("/jobs"))
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		var result struct {
			Jobs []struct {
				ID     uint   `json:"id"`
				Src    string `json:"src"`
				Dst    string `json:"dst"`
				Status string `json:"status"`
			} `json:"jobs"`
			Running map[string]struct {
				Synced int `json:"synced"`
				Failed int `json:"failed"`
			} `json:"running"`
		}

		_ = json.NewDecoder(resp.Body).Decode(&result)

		if len(result.Jobs) == 0 {
			fmt.Println("no jobs configured")
			return nil
		}

		fmt.Printf("%-4s %-8s %-30s %-30s %s\n", "ID", "STATUS", "SRC", "DST", "SYNCED/FAILED")
		for _, j := range result.Jobs {
			synced, failed := 0, 0
			if r, ok := result.Running[fmt.Sprint(j.ID)]; ok {
				synced = r.Synced
				failed = r.Failed
			}
			fmt.Printf("%-4d %-8s %-30s %-30s %d/%d\n", j.ID, j.Status, j.Src, j.Dst, synced, failed)
		}

		return nil
	},
}

var jobAddCmd = &cobra.Command{
	Use:   "add [src] [dst]",
	Short: "Add a new job",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcRaw, dstRaw := args[0], args[1]

		srcType := endpointType(srcRaw)
		dstType := endpointType(dstRaw)

		body := fmt.Sprintf(`{"src":"%s","src_type":"%s","dst":"%s","dst_type":"%s"}`,
			srcRaw, srcType, dstRaw, dstType)
		resp, err := http.Post(daemonURL("/jobs"), "application/json", strings.NewReader(body))

		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		fmt.Printf("job added: id=%v  %s → %s\n", result["id"], srcRaw, dstRaw)
		return nil
	},
}

var jobRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, _ := http.NewRequest(http.MethodDelete, daemonURL("/jobs/"+args[0]), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		fmt.Printf("job %s removed\n", args[0])
		return nil
	},
}

var jobPauseCmd = &cobra.Command{
	Use:   "pause [id]",
	Short: "Pause a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Post(daemonURL("/jobs/"+args[0]+"/pause"), "application/json", nil)
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		fmt.Printf("job %s paused\n", args[0])
		return nil
	},
}

var jobResumeCmd = &cobra.Command{
	Use:   "resume [id]",
	Short: "Resume a job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := http.Post(daemonURL("/jobs/"+args[0]+"/resume"), "application/json", nil)
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		fmt.Printf("job %s resumed\n", args[0])
		return nil
	},
}

func endpointType(raw string) model.EndpointType {
	switch {
	case strings.HasPrefix(raw, "gdrive:"):
		return model.EndpointGDrive
	case strings.HasPrefix(raw, "dropbox:"):
		return model.EndpointDropbox
	case tcp.ParseEndpoint(raw).IsRemote():
		return model.EndpointRemoteTCP
	default:
		return model.EndpointLocal
	}
}

func init() {
	jobCmd.AddCommand(jobListCmd, jobAddCmd, jobRemoveCmd, jobPauseCmd, jobResumeCmd)
	rootCmd.AddCommand(jobCmd)
}
