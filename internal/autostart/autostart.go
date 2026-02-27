package autostart

import "runtime"

type AutoStarter interface {
	Install(execPath string) error
	Uninstall() error
	IsInstalled() (bool, error)
}

func New() AutoStarter {
	switch runtime.GOOS {
	case "windows":
		return &WindowsAutoStarter{}
	case "linux":
		return &LinuxAutoStarter{}
	default:
		return &UnsupportedAutoStarter{}
	}
}

type UnsupportedAutoStarter struct{}

func (u *UnsupportedAutoStarter) Install(_ string) error {
	return nil
}

func (u *UnsupportedAutoStarter) Uninstall() error {
	return nil
}

func (u *UnsupportedAutoStarter) IsInstalled() (bool, error) {
	return false, nil
}
