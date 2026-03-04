package autostart

type unsupportedAutoStart struct{}

func (u *unsupportedAutoStart) Install(_ string, _ []string) error {
	return nil
}

func (u *unsupportedAutoStart) Uninstall() error {
	return nil
}

func (u *unsupportedAutoStart) IsInstalled() bool {
	return false
}

func (u *unsupportedAutoStart) Stop() error {
	return nil
}
