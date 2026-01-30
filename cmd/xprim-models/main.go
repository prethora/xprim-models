// Command xprim-models is a test CLI harness for the models package.
// It demonstrates the CLI integration and provides a working example.
//
// Configuration is loaded from environment variables:
//   - XPRIM_REGISTRY_URL: Base URL of the model registry (required)
//   - XPRIM_MODELS_DIR: Override for data directory (optional)
package main

import (
	"errors"
	"fmt"
	"os"

	models "github.com/prethora/xprim-models"
)

// CLI exit codes for standardized error reporting.
const (
	// ExitSuccess indicates the operation completed successfully.
	ExitSuccess = 0

	// ExitGeneralError indicates an unspecified error occurred.
	ExitGeneralError = 1

	// ExitInvalidArgs indicates invalid command line arguments.
	ExitInvalidArgs = 2

	// ExitModelNotFound indicates the model was not found in the registry.
	ExitModelNotFound = 3

	// ExitNotInstalled indicates the model is not installed locally.
	ExitNotInstalled = 4

	// ExitNetworkError indicates a network or connection failure.
	ExitNetworkError = 5

	// ExitHashMismatch indicates hash verification failed.
	ExitHashMismatch = 6

	// ExitStorageError indicates a filesystem operation failed.
	ExitStorageError = 7
)

func main() {
	// Get registry URL from environment
	registryURL := os.Getenv("XPRIM_REGISTRY_URL")
	if registryURL == "" {
		fmt.Fprintln(os.Stderr, "Error: XPRIM_REGISTRY_URL environment variable is required")
		os.Exit(ExitInvalidArgs)
	}

	cfg := models.Config{
		AppName:     "xprim",
		RegistryURL: registryURL,
		// DataDir can be set via XPRIM_MODELS_DIR env var (handled by storage layer)
	}

	cmd := models.NewCommand(cfg)
	if err := cmd.Execute(); err != nil {
		os.Exit(exitCodeFromError(err))
	}
}

// exitCodeFromError maps error types to exit codes.
func exitCodeFromError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	switch {
	case errors.Is(err, models.ErrModelNotFound):
		return ExitModelNotFound
	case errors.Is(err, models.ErrVersionNotFound):
		return ExitModelNotFound
	case errors.Is(err, models.ErrNotInstalled):
		return ExitNotInstalled
	case errors.Is(err, models.ErrNetworkError):
		return ExitNetworkError
	case errors.Is(err, models.ErrHashMismatch):
		return ExitHashMismatch
	case errors.Is(err, models.ErrStorageError):
		return ExitStorageError
	case errors.Is(err, models.ErrInvalidRef):
		return ExitInvalidArgs
	default:
		return ExitGeneralError
	}
}
