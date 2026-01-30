package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultLockTimeout is the default timeout for acquiring file locks.
const DefaultLockTimeout = 30 * time.Second

// localRegistry represents the contents of the local registry.json file.
// Structure: group → model → version → entry
type localRegistry map[string]map[string]map[string]installedModelEntry

// installedModelEntry represents a single entry in the local registry.
type installedModelEntry struct {
	// ManifestHash is the SHA-256 hash of the installed manifest.
	ManifestHash string `json:"manifest_hash"`

	// TotalSize is the total size of all model files in bytes.
	TotalSize int64 `json:"total_size"`

	// FileCount is the number of files in the model.
	FileCount int `json:"file_count"`

	// InstalledAt is when the model was installed.
	InstalledAt time.Time `json:"installed_at"`
}

// storageInterface defines operations for local filesystem management.
// Implemented by *storage for production and mockStorage for tests.
// This interface enables test isolation without filesystem dependencies.
type storageInterface interface {
	// loadRegistry reads and parses the local registry.json file.
	loadRegistry() (localRegistry, error)

	// saveRegistry atomically writes the registry to registry.json.
	saveRegistry(reg localRegistry) error

	// modelPath returns the absolute path to a model's directory.
	modelPath(ref ModelRef) string

	// chunkCachePath returns the path to the chunk cache directory.
	chunkCachePath() string

	// ensureDir creates a directory and all parent directories if they don't exist.
	ensureDir(path string) error

	// atomicWrite writes data to a file using write-then-rename for atomicity.
	atomicWrite(path string, data []byte) error

	// removeModelDir removes a model's directory and all its contents.
	removeModelDir(ref ModelRef) error

	// saveManifest stores a copy of the manifest in .{appName}/manifest.json within the model directory.
	saveManifest(ref ModelRef, mf manifest) error

	// removeChunkCache removes the chunk cache directory.
	removeChunkCache() error
}

// storage handles all local filesystem operations.
// Implements storageInterface.
type storage struct {
	// baseDir is the base directory for all storage operations.
	baseDir string

	// appName is the application name, used for the manifest metadata directory.
	appName string

	// lockTimeout is the maximum duration to wait for file lock acquisition.
	lockTimeout time.Duration

	// registryMu protects concurrent in-process access to registry.json.
	registryMu sync.RWMutex
}

// Ensure storage implements storageInterface.
var _ storageInterface = (*storage)(nil)

// envVarName constructs an environment variable name from the app name.
// Converts appName to uppercase and appends "_MODELS_DIR".
// Example: envVarName("xprim") returns "XPRIM_MODELS_DIR".
func envVarName(appName string) string {
	return strings.ToUpper(appName) + "_MODELS_DIR"
}

// newStorage creates a new storage instance for the given configuration.
func newStorage(cfg Config) (*storage, error) {
	var baseDir string

	// Priority: env var > Config.DataDir > platform default
	if envDir := os.Getenv(envVarName(cfg.AppName)); envDir != "" {
		baseDir = envDir
	} else if cfg.DataDir != "" {
		baseDir = cfg.DataDir
	} else {
		defaultDir, err := getDefaultDataDir(cfg.AppName)
		if err != nil {
			return nil, fmt.Errorf("failed to get default data dir: %w", err)
		}
		baseDir = defaultDir
	}

	s := &storage{baseDir: baseDir, appName: cfg.AppName, lockTimeout: DefaultLockTimeout}

	// Ensure base directory exists
	if err := s.ensureDir(baseDir); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	return s, nil
}

// loadRegistry reads and parses the local registry.json file.
// Returns an empty registry if the file doesn't exist.
func (s *storage) loadRegistry() (localRegistry, error) {
	s.registryMu.RLock()
	defer s.registryMu.RUnlock()

	path := filepath.Join(s.baseDir, "registry.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(localRegistry), nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStorageError, err)
	}

	var reg localRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("%w: invalid registry.json: %v", ErrStorageError, err)
	}

	return reg, nil
}

// saveRegistry atomically writes the registry to registry.json.
// Uses cross-process file locking to prevent concurrent writes from multiple processes.
func (s *storage) saveRegistry(reg localRegistry) error {
	s.registryMu.Lock()
	defer s.registryMu.Unlock()

	// Acquire cross-process file lock
	lockPath := filepath.Join(s.baseDir, "registry.json.lock")
	lock, err := newFileLock(lockPath, s.lockTimeout)
	if err != nil {
		return fmt.Errorf("%w: failed to create lock: %v", ErrStorageError, err)
	}
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("%w: failed to acquire lock: %v", ErrStorageError, err)
	}
	defer lock.Unlock()

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: failed to marshal registry: %v", ErrStorageError, err)
	}

	path := filepath.Join(s.baseDir, "registry.json")
	return s.atomicWriteInternal(path, data)
}

// atomicWriteInternal is the internal implementation without locking.
func (s *storage) atomicWriteInternal(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("%w: failed to create directory: %v", ErrStorageError, err)
	}

	// Write to temp file first
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("%w: failed to write temp file: %v", ErrStorageError, err)
	}

	// Atomic rename
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) // cleanup on failure
		return fmt.Errorf("%w: failed to rename temp file: %v", ErrStorageError, err)
	}

	return nil
}

// atomicWrite writes data to a file using write-then-rename for atomicity.
func (s *storage) atomicWrite(path string, data []byte) error {
	return s.atomicWriteInternal(path, data)
}

// modelPath returns the absolute path to a model's directory.
func (s *storage) modelPath(ref ModelRef) string {
	return filepath.Join(s.baseDir, ref.Group, ref.Model, ref.Version)
}

// chunkCachePath returns the path to the chunk cache directory.
func (s *storage) chunkCachePath() string {
	return filepath.Join(s.baseDir, ".chunks")
}

// ensureDir creates a directory and all parent directories if they don't exist.
func (s *storage) ensureDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("%w: failed to create directory %s: %v", ErrStorageError, path, err)
	}
	return nil
}

// removeModelDir removes a model's directory and all its contents.
func (s *storage) removeModelDir(ref ModelRef) error {
	path := s.modelPath(ref)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("%w: failed to remove model directory: %v", ErrStorageError, err)
	}
	return nil
}

// saveManifest stores a copy of the manifest in .{appName}/manifest.json within the model directory.
// This location is chosen to minimize conflicts with model files.
func (s *storage) saveManifest(ref ModelRef, mf manifest) error {
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: failed to marshal manifest: %v", ErrStorageError, err)
	}

	// Create metadata directory: .{appName}/
	metaDir := filepath.Join(s.modelPath(ref), "."+s.appName)
	if err := s.ensureDir(metaDir); err != nil {
		return fmt.Errorf("%w: failed to create metadata directory: %v", ErrStorageError, err)
	}

	path := filepath.Join(metaDir, "manifest.json")
	return s.atomicWrite(path, data)
}

// removeChunkCache removes the chunk cache directory.
func (s *storage) removeChunkCache() error {
	path := s.chunkCachePath()
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("%w: failed to remove chunk cache: %v", ErrStorageError, err)
	}
	return nil
}
