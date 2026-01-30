//go:build linux

package models

import (
	"os"
	"path/filepath"
)

// getDefaultDataDir returns the default data directory for Linux.
// Uses $XDG_DATA_HOME/<appName>/models/ if set,
// otherwise ~/.local/share/<appName>/models/
func getDefaultDataDir(appName string) (string, error) {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return filepath.Join(xdgData, appName, "models"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName, "models"), nil
}
