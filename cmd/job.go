package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"synco/internal/logger"
	"synco/internal/model"
	"synco/internal/repository"
	"synco/internal/syncer"
	"synco/internal/syncer/dropbox"
	"synco/internal/syncer/gdrive"
	"synco/internal/syncer/local"
	"synco/internal/syncer/tcp"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var jobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manage sync jobs",
}

// ── job list ────────────────────────────────────────────────────────────────

var jobListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured jobs",
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
				Src    string `json:"src_path"`
				Dst    string `json:"dst_path"`
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

// ── job add ──────────────────────────────────────────────────────────────────

var (
	jobAddOnce       bool
	jobAddForeground bool
)

var jobAddCmd = &cobra.Command{
	Use:   "add [src] [dst]",
	Short: "Add a sync job",
	Long: `Add a sync job. 

By default, the job is registered with the running daemon (which is started automatically if not already running).

Flags:
	--once			Perform a one-time sync immediately and exit (local→local only)
	--foreground	Run the daemon in the foreground for this session`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		src, dst := args[0], args[1]

		switch {
		case jobAddOnce:
			return runSyncOnce(src, dst)
		case jobAddForeground:
			return runForegroundDaemon(src, dst)
		default:
			return addJob(src, dst)
		}
	},
}

func addJob(src, dst string) error {
	if !isDaemonRunning() {
		_, _ = fmt.Fprintln(os.Stderr, "synco daemon is not running")
		_, _ = fmt.Fprintln(os.Stderr, "  start on login (recommended):  synco install")
		_, _ = fmt.Fprintln(os.Stderr, "  start manually this session:   synco daemon start")
		return fmt.Errorf("daemon not running")
	}

	return postJob(src, dst)
}

func runSyncOnce(src, dst string) error {
	s, err := buildFullSyncer(src, dst)
	if err != nil {
		return err
	}

	logger.Log.Info("starting one-time sync",
		zap.String("src", src),
		zap.String("dst", dst))

	results, err := s.FullSync()
	if err != nil {
		return err
	}

	repo := repository.NewHistoryRepository()
	var synced, failed int
	for _, r := range results {
		_ = repo.Save(r, 0)
		if r.Err != nil {
			failed++
			fmt.Printf("  ✗ %s: %v\n", r.SrcPath, r.Err)
		} else {
			synced++
		}
	}

	fmt.Printf("done : %d synced, %d failed\n", synced, failed)
	return nil
}

func runForegroundDaemon(src, dst string) error {
	if isDaemonRunning() {
		return fmt.Errorf("daemon is already running\n"+
			"  use 'synco job add %s %s' to register the job", src, dst)
	}

	return runDaemonInProcess(src, dst)
}

func buildFullSyncer(src, dst string) (syncer.Syncer, error) {
	srcType := endpointType(src)
	dstType := endpointType(dst)

	nodeID, err := model.LoadOrCreateNodeID()
	if err != nil {
		return nil, err
	}

	switch {
	case srcType == model.EndpointLocal && dstType == model.EndpointLocal:
		return local.NewSyncer(src, dst, cfg.ConflictStrategy)

	case srcType == model.EndpointLocal && dstType == model.EndpointRemoteTCP:
		return tcp.NewSyncer(src, dst, nodeID, tcp.NewVclock())

	case srcType == model.EndpointLocal && dstType == model.EndpointGDrive:
		path := strings.TrimPrefix(dst, "gdrive:")
		return gdrive.NewUploader(src, path)

	case srcType == model.EndpointLocal && dstType == model.EndpointDropbox:
		path := strings.TrimPrefix(dst, "dropbox:")
		return dropbox.NewUploader(src, path)

	case srcType == model.EndpointGDrive && dstType == model.EndpointLocal:
		path := strings.TrimPrefix(src, "gdrive:")
		return gdrive.NewDownloader(path, dst)

	case srcType == model.EndpointDropbox && dstType == model.EndpointLocal:
		path := strings.TrimPrefix(src, "dropbox:")
		return dropbox.NewDownloader(path, dst)

	default:
		return nil, fmt.Errorf("--once does not support %s → %s", srcType, dstType)
	}
}

// ── job remove / pause / resume ───────────────────────────────────────────────

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

// ── helpers ───────────────────────────────────────────────────────────────────

func isDaemonRunning() bool {
	resp, err := http.Get(daemonURL("/status"))
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func postJob(src, dst string) error {
	srcType := endpointType(src)
	dstType := endpointType(dst)

	body := fmt.Sprintf(`{"src":"%s","src_type":"%s","dst":"%s","dst_type":"%s"}`,
		src, srcType, dst, dstType)
	resp, err := http.Post(daemonURL("/jobs"), "application/json", strings.NewReader(body))

	if err != nil {
		return fmt.Errorf("failed to reach daemon: %w", err)
	}

	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("job added: id=%v  %s → %s\n", result["id"], src, dst)
	return nil
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
	jobAddCmd.Flags().BoolVar(&jobAddOnce, "once", false, "sync once and exit (local→local only)")
	jobAddCmd.Flags().BoolVar(&jobAddForeground, "foreground", false, "run daemon in foreground")

	jobCmd.AddCommand(jobListCmd, jobAddCmd, jobRemoveCmd, jobPauseCmd, jobResumeCmd)
	rootCmd.AddCommand(jobCmd)
}
