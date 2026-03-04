package autostart

import "runtime"

type AutoStart interface {
	Install(execPath string, args []string) error
	Uninstall() error
	IsInstalled() bool
	Stop() error
}

func New() AutoStart {
	switch runtime.GOOS {
	case "windows":
		return newWindows()
	case "linux":
		return newLinux()
	default:
		return &unsupportedAutoStart{}
	}
}
