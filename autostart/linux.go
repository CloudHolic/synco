package autostart

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const serviceTemplate = `[Unit]
Description=Synco File Sync Daemon
After=network.target

[SErvice]
ExecStart={{.ExecPath}} watch
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`

type LinuxAutoStarter struct{}

func (l *LinuxAutoStarter) servicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	return filepath.Join(dir, "synco.service"), nil
}

func (l *LinuxAutoStarter) Install(execPath string) error {
	path, err := l.servicePath()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create service file: %w", err)
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	tmpl := template.Must(template.New("service").Parse(serviceTemplate))
	if err := tmpl.Execute(f, map[string]string{"ExecPath": execPath}); err != nil {
		return fmt.Errorf("failed to write service file: %w", err)
	}

	cmds := [][]string{
		{"systemctl", "--user", "daemon-reload"},
		{"systemctl", "--user", "enable", "synco.service"},
		{"systemctl", "--user", "start", "synco.service"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to run %v: %w\n%s", args, err, out)
		}
	}

	return nil
}

func (l *LinuxAutoStarter) Uninstall() error {
	cmds := [][]string{
		{"systemctl", "--user", "stop", "synco.service"},
		{"systemctl", "--user", "disable", "synco.service"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		_ = cmd.Run()
	}

	path, err := l.servicePath()
	if err != nil {
		return err
	}

	return os.Remove(path)
}

func (l *LinuxAutoStarter) IsInstalled() (bool, error) {
	path, err := l.servicePath()
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	return err == nil, nil
}
