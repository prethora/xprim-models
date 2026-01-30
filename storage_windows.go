//go:build windows

package models

import (
	"os"
	"path/filepath"
)

// getDefaultDataDir returns the default data directory for Windows.
// Returns %APPDATA%\<appName>\models\
func getDefaultDataDir(appName string) (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, appName, "models"), nil
}
