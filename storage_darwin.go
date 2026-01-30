//go:build darwin

package models

import (
	"os"
	"path/filepath"
)

// getDefaultDataDir returns the default data directory for macOS.
// Returns ~/Library/Application Support/<appName>/models/
func getDefaultDataDir(appName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", appName, "models"), nil
}
