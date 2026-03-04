package autostart

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

type linuxAutoStart struct {
	serviceName string
	servicePath string
}

func newLinux() AutoStart {
	home, _ := os.UserHomeDir()
	return &linuxAutoStart{
		serviceName: "synco",
		servicePath: filepath.Join(home, ".config", "systemd", "user", "synco.service"),
	}
}

func (a *linuxAutoStart) Install(execPath string, args []string) error {
	if err := os.MkdirAll(filepath.Dir(a.servicePath), 0755); err != nil {
		return err
	}

	f, err := os.Create(a.servicePath)
	if err != nil {
		return err
	}

	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	tmpl := template.Must(template.New("service").Parse(serviceTemplate))
	if err := tmpl.Execute(f, map[string]string{
		"ExEcPath": execPath,
		"Args":     strings.Join(args, " "),
	}); err != nil {
		return err
	}

	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return err
	}

	return exec.Command("systemctl", "--user", "enable", "--now", a.serviceName).Run()
}

func (a *linuxAutoStart) Uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", a.serviceName).Run()
	return os.Remove(a.servicePath)
}

func (a *linuxAutoStart) IsInstalled() bool {
	_, err := os.Stat(a.servicePath)
	return err == nil
}

func (a *linuxAutoStart) Stop() error {
	return exec.Command("systemctl", "--user", "stop", a.serviceName).Run()
}
