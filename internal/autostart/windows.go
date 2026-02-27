package autostart

import (
	"fmt"
	"os/exec"
)

const taskName = "SyncoDaemon"

type WindowsAutoStarter struct{}

func (w *WindowsAutoStarter) Install(execPath string) error {
	cmd := exec.Command("schtasks", "/create",
		"/TN", taskName,
		"/TR", fmt.Sprintf(`"%s" watch`, execPath),
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to register task: %w\n%s", err, out)
	}

	return nil
}

func (w *WindowsAutoStarter) Uninstall() error {
	cmd := exec.Command("schtasks", "/DELETE", "/TN", taskName, "/F")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove task: %w\n%s", err, out)
	}

	return nil
}

func (w *WindowsAutoStarter) IsInstalled() (bool, error) {
	cmd := exec.Command("schtasks", "/Query", "/TN", taskName)
	err := cmd.Run()
	if err != nil {
		return false, nil
	}

	return true, nil
}
