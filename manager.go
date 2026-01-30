package models

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// manager is the concrete implementation of the Manager interface.
type manager struct {
	// cfg holds the module configuration.
	cfg Config

	// httpClient is used for all HTTP requests.
	httpClient HTTPClient

	// logger receives diagnostic messages. May be nil.
	logger Logger

	// storage handles local filesystem operations.
	storage storageInterface

	// registry handles remote registry communication.
	registry *registryClient

	// downloadMu serializes pull operations for the same model.
	downloadMu sync.Mutex
}

// ListInstalled returns all locally installed models.
func (m *manager) ListInstalled(ctx context.Context) ([]InstalledModel, error) {
	reg, err := m.storage.loadRegistry()
	if err != nil {
		return nil, fmt.Errorf("loading registry: %w", err)
	}

	var models []InstalledModel
	for group, groupModels := range reg {
		for model, versions := range groupModels {
			for version, entry := range versions {
				ref := ModelRef{Group: group, Model: model, Version: version}
				models = append(models, InstalledModel{
					Ref:          ref,
					ManifestHash: entry.ManifestHash,
					TotalSize:    entry.TotalSize,
					FileCount:    entry.FileCount,
					InstalledAt:  entry.InstalledAt,
					Path:         m.storage.modelPath(ref),
				})
			}
		}
	}

	return models, nil
}

// ListRemote fetches and returns all models available in the registry.
func (m *manager) ListRemote(ctx context.Context) ([]RemoteModel, error) {
	index, err := m.registry.fetchModelsIndex(ctx)
	if err != nil {
		return nil, err
	}

	var models []RemoteModel
	for groupName, groupModels := range index {
		for modelName, modelVersions := range groupModels {
			for versionName, version := range modelVersions {
				models = append(models, RemoteModel{
					Ref: ModelRef{
						Group:   groupName,
						Model:   modelName,
						Version: versionName,
					},
					ManifestHash: version.Hash,
					Metadata:     version.Metadata,
				})
			}
		}
	}

	return models, nil
}

// ListRemoteGroup returns all models in a specific group.
func (m *manager) ListRemoteGroup(ctx context.Context, groupName string) ([]RemoteModel, error) {
	index, err := m.registry.fetchModelsIndex(ctx)
	if err != nil {
		return nil, err
	}

	groupModels, ok := index[groupName]
	if !ok {
		return []RemoteModel{}, nil
	}

	var models []RemoteModel
	for modelName, modelVersions := range groupModels {
		for versionName, version := range modelVersions {
			models = append(models, RemoteModel{
				Ref: ModelRef{
					Group:   groupName,
					Model:   modelName,
					Version: versionName,
				},
				ManifestHash: version.Hash,
				Metadata:     version.Metadata,
			})
		}
	}

	return models, nil
}

// ListRemoteVersions returns all versions of a specific model.
func (m *manager) ListRemoteVersions(ctx context.Context, groupName, modelName string) ([]RemoteModel, error) {
	index, err := m.registry.fetchModelsIndex(ctx)
	if err != nil {
		return nil, err
	}

	groupModels, ok := index[groupName]
	if !ok {
		return []RemoteModel{}, nil
	}

	modelVersions, ok := groupModels[modelName]
	if !ok {
		return []RemoteModel{}, nil
	}

	var models []RemoteModel
	for versionName, version := range modelVersions {
		models = append(models, RemoteModel{
			Ref: ModelRef{
				Group:   groupName,
				Model:   modelName,
				Version: versionName,
			},
			ManifestHash: version.Hash,
			Metadata:     version.Metadata,
		})
	}

	return models, nil
}

// GetInstalled returns info about a specific installed model.
func (m *manager) GetInstalled(ctx context.Context, ref ModelRef) (InstalledModel, error) {
	reg, err := m.storage.loadRegistry()
	if err != nil {
		return InstalledModel{}, fmt.Errorf("loading registry: %w", err)
	}

	groupModels, ok := reg[ref.Group]
	if !ok {
		return InstalledModel{}, ErrNotInstalled
	}

	versions, ok := groupModels[ref.Model]
	if !ok {
		return InstalledModel{}, ErrNotInstalled
	}

	entry, ok := versions[ref.Version]
	if !ok {
		return InstalledModel{}, ErrNotInstalled
	}

	return InstalledModel{
		Ref:          ref,
		ManifestHash: entry.ManifestHash,
		TotalSize:    entry.TotalSize,
		FileCount:    entry.FileCount,
		InstalledAt:  entry.InstalledAt,
		Path:         m.storage.modelPath(ref),
	}, nil
}

// GetRemote returns info about a specific model from the registry.
func (m *manager) GetRemote(ctx context.Context, ref ModelRef) (RemoteModel, error) {
	index, err := m.registry.fetchModelsIndex(ctx)
	if err != nil {
		return RemoteModel{}, err
	}

	groupModels, ok := index[ref.Group]
	if !ok {
		return RemoteModel{}, ErrModelNotFound
	}

	modelVersions, ok := groupModels[ref.Model]
	if !ok {
		return RemoteModel{}, ErrModelNotFound
	}

	version, ok := modelVersions[ref.Version]
	if !ok {
		return RemoteModel{}, ErrVersionNotFound
	}

	return RemoteModel{
		Ref:          ref,
		ManifestHash: version.Hash,
		Metadata:     version.Metadata,
	}, nil
}

// GetRemoteDetail fetches the manifest and returns detailed model info.
func (m *manager) GetRemoteDetail(ctx context.Context, ref ModelRef) (RemoteModelDetail, error) {
	remote, err := m.GetRemote(ctx, ref)
	if err != nil {
		return RemoteModelDetail{}, err
	}

	mf, err := m.registry.fetchManifest(ctx, remote.ManifestHash)
	if err != nil {
		return RemoteModelDetail{}, err
	}

	files := make([]FileInfo, len(mf.Files))
	for i, f := range mf.Files {
		files[i] = FileInfo{Path: f.Path, Size: f.Size}
	}

	return RemoteModelDetail{
		RemoteModel: remote,
		TotalSize:   mf.TotalSize,
		ChunkSize:   mf.ChunkSize,
		ChunkCount:  len(mf.Chunks),
		Files:       files,
	}, nil
}

// Pull downloads and installs a model from the registry.
func (m *manager) Pull(ctx context.Context, ref ModelRef, opts ...PullOption) error {
	cfg := &pullConfig{
		concurrency: DefaultConcurrency,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	m.downloadMu.Lock()
	defer m.downloadMu.Unlock()

	// Acquire cross-process lock for this specific model
	// This prevents concurrent pulls of the same model from different processes
	modelDir := m.storage.modelPath(ref)
	if err := m.storage.ensureDir(modelDir); err != nil {
		return fmt.Errorf("creating model directory: %w", err)
	}
	lockPath := filepath.Join(modelDir, ".pull.lock")
	pullLock, err := newFileLock(lockPath, DefaultLockTimeout)
	if err != nil {
		return fmt.Errorf("%w: failed to create pull lock: %v", ErrStorageError, err)
	}
	if err := pullLock.Lock(); err != nil {
		return fmt.Errorf("%w: another process is pulling this model: %v", ErrStorageError, err)
	}
	defer pullLock.Unlock()

	// Check if already installed
	if !cfg.force {
		installed, err := m.GetInstalled(ctx, ref)
		if err == nil {
			// Already installed, check if same version
			remote, err := m.GetRemote(ctx, ref)
			if err != nil {
				return err
			}
			if installed.ManifestHash == remote.ManifestHash {
				return ErrAlreadyInstalled
			}
		}
	}

	// Get remote model info
	remote, err := m.GetRemote(ctx, ref)
	if err != nil {
		return err
	}

	// Report progress: manifest phase
	if cfg.progressFn != nil {
		cfg.progressFn(PullProgress{
			Phase: "manifest",
		})
	}

	// Fetch manifest
	mf, err := m.registry.fetchManifest(ctx, remote.ManifestHash)
	if err != nil {
		return err
	}

	// Report progress: chunks phase
	if cfg.progressFn != nil {
		cfg.progressFn(PullProgress{
			Phase:       "chunks",
			ChunksTotal: len(mf.Chunks),
			BytesTotal:  mf.TotalSize,
		})
	}

	// Download chunks
	engine := newDownloadEngine(m.registry, m.storage, m.logger)
	chunksCompleted := 0
	err = engine.downloadChunks(ctx, mf.Chunks, cfg.concurrency, func(completed, total int, bytesDownloaded, bytesInProgress int64) {
		chunksCompleted = completed
		if cfg.progressFn != nil {
			// Calculate bytes completed, capping at total size
			// (last chunk is typically smaller than chunk size)
			bytesCompleted := int64(completed) * mf.ChunkSize
			if bytesCompleted > mf.TotalSize {
				bytesCompleted = mf.TotalSize
			}
			cfg.progressFn(PullProgress{
				Phase:           "chunks",
				ChunksTotal:     total,
				ChunksCompleted: completed,
				BytesTotal:      mf.TotalSize,
				BytesCompleted:  bytesCompleted,
				BytesDownloaded: bytesDownloaded,
				BytesInProgress: bytesInProgress,
			})
		}
	})
	if err != nil {
		return fmt.Errorf("downloading chunks: %w", err)
	}

	// Report progress: extracting phase
	if cfg.progressFn != nil {
		cfg.progressFn(PullProgress{
			Phase:           "extracting",
			ChunksTotal:     len(mf.Chunks),
			ChunksCompleted: chunksCompleted,
			BytesTotal:      mf.TotalSize,
			BytesCompleted:  mf.TotalSize,
		})
	}

	// Reconstruct files
	cache := newChunkCache(m.storage.chunkCachePath())
	reconstructor := newFileReconstructor(cache, m.storage)
	err = reconstructor.reconstruct(ctx, mf, ref, func(currentFile string) {
		if cfg.progressFn != nil {
			cfg.progressFn(PullProgress{
				Phase:           "extracting",
				ChunksTotal:     len(mf.Chunks),
				ChunksCompleted: len(mf.Chunks),
				BytesTotal:      mf.TotalSize,
				BytesCompleted:  mf.TotalSize,
				CurrentFile:     currentFile,
			})
		}
	})
	if err != nil {
		return fmt.Errorf("reconstructing files: %w", err)
	}

	// Save manifest copy
	if err := m.storage.saveManifest(ref, mf); err != nil {
		if m.logger != nil {
			m.logger.Warn("failed to save manifest copy", "error", err)
		}
	}

	// Update registry
	reg, err := m.storage.loadRegistry()
	if err != nil {
		return fmt.Errorf("loading registry: %w", err)
	}

	if reg[ref.Group] == nil {
		reg[ref.Group] = make(map[string]map[string]installedModelEntry)
	}
	if reg[ref.Group][ref.Model] == nil {
		reg[ref.Group][ref.Model] = make(map[string]installedModelEntry)
	}

	reg[ref.Group][ref.Model][ref.Version] = installedModelEntry{
		ManifestHash: remote.ManifestHash,
		TotalSize:    mf.TotalSize,
		FileCount:    len(mf.Files),
		InstalledAt:  time.Now(),
	}

	if err := m.storage.saveRegistry(reg); err != nil {
		return fmt.Errorf("saving registry: %w", err)
	}

	return nil
}

// Remove deletes a locally installed model.
func (m *manager) Remove(ctx context.Context, ref ModelRef) error {
	reg, err := m.storage.loadRegistry()
	if err != nil {
		return fmt.Errorf("loading registry: %w", err)
	}

	// Check if installed
	groupModels, ok := reg[ref.Group]
	if !ok {
		return ErrNotInstalled
	}

	versions, ok := groupModels[ref.Model]
	if !ok {
		return ErrNotInstalled
	}

	if _, ok := versions[ref.Version]; !ok {
		return ErrNotInstalled
	}

	// Remove model directory
	if err := m.storage.removeModelDir(ref); err != nil {
		return fmt.Errorf("removing model directory: %w", err)
	}

	// Update registry
	delete(versions, ref.Version)

	// Clean up empty maps
	if len(versions) == 0 {
		delete(groupModels, ref.Model)
	}
	if len(groupModels) == 0 {
		delete(reg, ref.Group)
	}

	if err := m.storage.saveRegistry(reg); err != nil {
		return fmt.Errorf("saving registry: %w", err)
	}

	return nil
}

// RemoveAll deletes all versions of a model.
func (m *manager) RemoveAll(ctx context.Context, group, model string) error {
	reg, err := m.storage.loadRegistry()
	if err != nil {
		return fmt.Errorf("loading registry: %w", err)
	}

	groupModels, ok := reg[group]
	if !ok {
		return nil // Nothing to remove
	}

	versions, ok := groupModels[model]
	if !ok {
		return nil // Nothing to remove
	}

	var firstErr error
	for version := range versions {
		ref := ModelRef{Group: group, Model: model, Version: version}
		if err := m.Remove(ctx, ref); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// Path returns the absolute path to an installed model's directory.
func (m *manager) Path(ctx context.Context, ref ModelRef) (string, error) {
	_, err := m.GetInstalled(ctx, ref)
	if err != nil {
		return "", err
	}

	return m.storage.modelPath(ref), nil
}

// CheckUpdate checks if a newer version is available for an installed model.
func (m *manager) CheckUpdate(ctx context.Context, ref ModelRef) (installedHash, remoteHash string, err error) {
	installed, err := m.GetInstalled(ctx, ref)
	if err != nil {
		return "", "", err
	}

	remote, err := m.GetRemote(ctx, ref)
	if err != nil {
		return "", "", err
	}

	return installed.ManifestHash, remote.ManifestHash, nil
}

// PruneCache removes all cached chunks from incomplete downloads.
func (m *manager) PruneCache(ctx context.Context) error {
	return m.storage.removeChunkCache()
}
