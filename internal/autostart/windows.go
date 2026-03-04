package autostart

import (
	"os/exec"
	"strings"
)

const taskName = "SyncoDaemon"

type windowsAutoStart struct {
	taskName string
}

func newWindows() AutoStart {
	return &windowsAutoStart{taskName: "synco"}
}

func (a *windowsAutoStart) Install(execPath string, args []string) error {
	return exec.Command("schtasks", "/create",
		"/tn", a.taskName,
		"/tr", execPath+" "+strings.Join(args, " "),
		"/sc", "onlogon",
		"/f",
	).Run()
}

func (a *windowsAutoStart) Uninstall() error {
	return exec.Command("schtasks", "/delete", "/tn", a.taskName, "/f").Run()
}

func (a *windowsAutoStart) IsInstalled() bool {
	err := exec.Command("schtasks", "/query", "/tn", a.taskName).Run()
	return err == nil
}

func (a *windowsAutoStart) Stop() error {
	_ = exec.Command("schtasks", "/change", "/tn", a.taskName, "/disable").Run()
	return nil
}
