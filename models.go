package models

import (
	"context"
	"errors"
	"net/http"
)

// Manager provides programmatic access to model management.
// All methods are safe for concurrent use.
// For CLI integration, use NewCommand instead.
type Manager interface {
	// ListInstalled returns all locally installed models.
	ListInstalled(ctx context.Context) ([]InstalledModel, error)

	// ListRemote fetches and returns all models available in the registry.
	ListRemote(ctx context.Context) ([]RemoteModel, error)

	// ListRemoteGroup returns all models in a specific group.
	ListRemoteGroup(ctx context.Context, group string) ([]RemoteModel, error)

	// ListRemoteVersions returns all versions of a specific model.
	ListRemoteVersions(ctx context.Context, group, model string) ([]RemoteModel, error)

	// GetInstalled returns info about a specific installed model.
	// Returns ErrNotInstalled if the model is not installed locally.
	GetInstalled(ctx context.Context, ref ModelRef) (InstalledModel, error)

	// GetRemote returns info about a specific model from the registry.
	// Returns ErrModelNotFound if the model does not exist.
	GetRemote(ctx context.Context, ref ModelRef) (RemoteModel, error)

	// GetRemoteDetail fetches the manifest and returns detailed model info.
	// This requires an additional HTTP request beyond GetRemote.
	GetRemoteDetail(ctx context.Context, ref ModelRef) (RemoteModelDetail, error)

	// Pull downloads and installs a model from the registry.
	// If the model is already installed with the same manifest hash, returns
	// ErrAlreadyInstalled unless WithForce() is specified.
	Pull(ctx context.Context, ref ModelRef, opts ...PullOption) error

	// Remove deletes a locally installed model.
	// Returns ErrNotInstalled if the model is not installed.
	Remove(ctx context.Context, ref ModelRef) error

	// RemoveAll deletes all versions of a model.
	// Continues removing remaining versions if one fails.
	RemoveAll(ctx context.Context, group, model string) error

	// Path returns the absolute path to an installed model's directory.
	// Returns ErrNotInstalled if the model is not installed.
	Path(ctx context.Context, ref ModelRef) (string, error)

	// CheckUpdate checks if a newer version is available for an installed model.
	// Returns the installed manifest hash and the current remote hash.
	// If they differ, an update is available.
	// Returns ErrNotInstalled if the model is not installed locally.
	CheckUpdate(ctx context.Context, ref ModelRef) (installedHash, remoteHash string, err error)

	// PruneCache removes all cached chunks from incomplete downloads.
	// This frees disk space but may require re-downloading chunks if a download is resumed.
	PruneCache(ctx context.Context) error
}

// Ensure manager implements Manager interface.
var _ Manager = (*manager)(nil)

// NewManager creates a new Manager with the given configuration.
// Returns an error if the configuration is invalid (empty AppName or RegistryURL).
func NewManager(cfg Config, opts ...ManagerOption) (Manager, error) {
	// Validate config
	if cfg.AppName == "" {
		return nil, errors.New("models: AppName is required")
	}
	if cfg.RegistryURL == "" {
		return nil, errors.New("models: RegistryURL is required")
	}

	// Apply options
	mcfg := &managerConfig{
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(mcfg)
	}

	// Create storage
	storage, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}

	// Create registry client
	registry := newRegistryClient(cfg.RegistryURL, mcfg.httpClient, mcfg.logger)

	return &manager{
		cfg:        cfg,
		httpClient: mcfg.httpClient,
		logger:     mcfg.logger,
		storage:    storage,
		registry:   registry,
	}, nil
}
