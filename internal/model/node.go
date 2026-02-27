package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

func LoadOrCreateNodeID() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	idPath := filepath.Join(home, ".synco", "node-id")

	if data, err := os.ReadFile(idPath); err == nil {
		return string(data), nil
	}

	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate node id: %w", err)
	}
	id := hex.EncodeToString(b)

	if err := os.WriteFile(idPath, []byte(id), 0644); err != nil {
		return "", fmt.Errorf("failed to save node id: %w", err)
	}

	return id, nil
}
