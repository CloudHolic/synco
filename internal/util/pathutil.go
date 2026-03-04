package util

import (
	"os"
	"path/filepath"
)

func SyncoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(home, ".synco")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}

	return dir, nil
}
