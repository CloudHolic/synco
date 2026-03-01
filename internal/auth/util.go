package auth

import (
	"os"
	"path/filepath"
)

func syncoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".synco"), nil
}
