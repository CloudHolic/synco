package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"synco/model"

	"github.com/spf13/cobra"
)

var historyN int

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "View synco history",
	RunE: func(cmd *cobra.Command, args []string) error {
		url := fmt.Sprintf("%s?n=%d", daemonURL("/history"), historyN)
		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("daemon not running: %w", err)
		}

		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		var histories []model.History
		if err := json.NewDecoder(resp.Body).Decode(&histories); err != nil {
			return err
		}

		if len(histories) == 0 {
			fmt.Println("no history yet")
			return nil
		}

		for _, h := range histories {
			status := "✓"
			if h.EventType == "FAILED" {
				status = "✗"
			}

			fmt.Printf("%s [%s] %-7s %s\n",
				status,
				h.SyncedAt.Format("2006-01-02 15:04:05"),
				h.FileEvent,
				h.SrcPath,
			)
		}

		return nil
	},
}

func init() {
	historyCmd.Flags().IntVar(&historyN, "n", 20, "number of history entries to show")
	rootCmd.AddCommand(historyCmd)
}
