package models

import "errors"

// Sentinel errors for model management operations.
// Use errors.Is() to check for specific error conditions.
var (
	// ErrModelNotFound indicates the model does not exist in the registry.
	ErrModelNotFound = errors.New("models: model not found in registry")

	// ErrVersionNotFound indicates the version does not exist for the model.
	ErrVersionNotFound = errors.New("models: version not found")

	// ErrNotInstalled indicates the model is not installed locally.
	ErrNotInstalled = errors.New("models: model not installed")

	// ErrAlreadyInstalled indicates the model is already installed.
	// Returned by Pull when model exists and WithForce() is not specified.
	ErrAlreadyInstalled = errors.New("models: model already installed")

	// ErrHashMismatch indicates downloaded data failed hash verification.
	ErrHashMismatch = errors.New("models: hash verification failed")

	// ErrNetworkError indicates a network or connection failure.
	ErrNetworkError = errors.New("models: network error")

	// ErrStorageError indicates a filesystem operation failed.
	ErrStorageError = errors.New("models: storage error")

	// ErrInvalidRef indicates an invalid model reference format.
	ErrInvalidRef = errors.New("models: invalid model reference")

	// ErrRegistryError indicates the registry returned invalid or unparseable data.
	ErrRegistryError = errors.New("models: invalid registry response")
)
