# Architecture: xprim-models Module

A Go module for consuming ML models from an XPRIM content-addressed model registry.

## 1. Package Overview

```
models/
├── models.go          # Public API: Manager interface, NewManager, NewCommand
├── types.go           # Public types: Config, ModelRef, InstalledModel, RemoteModel, etc.
├── options.go         # PullOption, ManagerOption, With* functions
├── errors.go          # Sentinel errors
├── manager.go         # Internal manager implementation
├── storage.go         # Storage abstraction (platform path logic)
├── storage_darwin.go  # macOS path resolution (//go:build darwin)
├── storage_linux.go   # Linux path resolution (//go:build linux)
├── storage_windows.go # Windows path resolution (//go:build windows)
├── lock_unix.go       # Cross-process file locking for Unix (//go:build !windows)
├── lock_windows.go    # Cross-process file locking for Windows (//go:build windows)
├── registry.go        # Registry client for HTTP communication
├── download.go        # Download engine and chunk management
├── reconstruct.go     # File reconstruction from chunks
└── cmd/
    └── xprim-models/
        └── main.go    # Test CLI harness
```

## 2. Shared Types

### 2.1 Errors (`errors.go`)

```go
// Package models provides functionality for consuming ML models from an
// XPRIM content-addressed model registry.
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
```

### 2.2 Core Types (`types.go`)

```go
package models

import "time"

// Config configures the models module.
type Config struct {
	// AppName determines the storage directory name.
	// Example: "xprim" → ~/.local/share/xprim/models/ on Linux
	AppName string

	// RegistryURL is the base URL of the model registry.
	// Example: "https://pub-abc123.r2.dev"
	RegistryURL string

	// DataDir overrides the default data directory.
	// If empty, uses platform-appropriate default.
	// Can also be set via environment variable: <APPNAME>_MODELS_DIR
	DataDir string
}

// ModelRef identifies a specific model version.
type ModelRef struct {
	// Group is the model group, e.g., "fast-whisper".
	Group string

	// Model is the model name within the group, e.g., "tiny".
	Model string

	// Version is the model version, e.g., "fp16", "int8", "q4".
	Version string
}

// String returns the canonical string form: "group/model version".
func (r ModelRef) String() string

// ParseModelRef parses "group/model" or "group/model version" into a ModelRef.
// Returns ErrInvalidRef if the format is invalid.
func ParseModelRef(s string) (ModelRef, error)

// FileInfo describes a file within a model.
type FileInfo struct {
	// Path is the relative path within the model directory.
	Path string

	// Size is the file size in bytes.
	Size int64
}

// InstalledModel contains information about a locally installed model.
type InstalledModel struct {
	// Ref identifies the model.
	Ref ModelRef

	// ManifestHash is the SHA-256 hash of the manifest.
	ManifestHash string

	// TotalSize is the total size in bytes of all model files.
	TotalSize int64

	// FileCount is the number of files in the model.
	FileCount int

	// InstalledAt is when the model was installed.
	InstalledAt time.Time

	// Path is the absolute path to the model directory.
	Path string
}

// RemoteModel contains information about a model available in the registry.
type RemoteModel struct {
	// Ref identifies the model.
	Ref ModelRef

	// ManifestHash is the SHA-256 hash of the manifest.
	ManifestHash string

	// Metadata contains optional arbitrary metadata from the registry.
	Metadata map[string]any
}

// RemoteModelDetail contains detailed information from a model's manifest.
type RemoteModelDetail struct {
	RemoteModel

	// TotalSize is the total size in bytes of all model files.
	TotalSize int64

	// ChunkSize is the size of each chunk in bytes.
	ChunkSize int64

	// ChunkCount is the number of chunks.
	ChunkCount int

	// Files lists all files contained in the model.
	Files []FileInfo
}

// PullProgress reports download progress during a pull operation.
type PullProgress struct {
	// Phase indicates the current phase: "manifest", "chunks", or "extracting".
	Phase string

	// ChunksTotal is the total number of chunks to download.
	ChunksTotal int

	// ChunksCompleted is the number of chunks downloaded so far.
	ChunksCompleted int

	// BytesTotal is the total bytes to download.
	BytesTotal int64

	// BytesCompleted is the bytes from completed chunks so far (for progress %).
	BytesCompleted int64

	// BytesDownloaded is cumulative bytes fetched from network (excludes cache hits).
	// This is bytes from completed chunk downloads.
	BytesDownloaded int64

	// BytesInProgress is bytes currently being downloaded across all workers.
	// Use BytesDownloaded + BytesInProgress for smooth, accurate speed calculation.
	BytesInProgress int64

	// CurrentFile is the file being processed during extraction phase.
	CurrentFile string
}
```

### 2.3 Options (`options.go`)

```go
package models

import (
	"net/http"
	"time"
)

// Concurrency constants for chunk downloads.
const (
	// DefaultConcurrency is the default number of concurrent chunk downloads.
	DefaultConcurrency = 4

	// MaxConcurrency is the maximum allowed concurrent chunk downloads.
	MaxConcurrency = 16

	// DefaultRequestTimeout is the default timeout for HTTP requests.
	DefaultRequestTimeout = 30 * time.Second
)

// Retry configuration constants for failed HTTP requests.
const (
	// MaxRetries is the maximum number of retry attempts for failed requests.
	MaxRetries = 3

	// InitialBackoff is the initial backoff duration before first retry.
	InitialBackoff = 1 * time.Second

	// MaxBackoff is the maximum backoff duration between retries.
	MaxBackoff = 4 * time.Second
)

// PullOption configures a pull operation.
type PullOption func(*pullConfig)

// pullConfig holds configuration for a pull operation.
type pullConfig struct {
	// force causes re-download even if model is already installed.
	force bool

	// concurrency is the number of concurrent chunk downloads.
	concurrency int

	// progressFn is called with progress updates during download.
	progressFn func(PullProgress)
}

// WithForce forces re-download even if the model is already installed.
func WithForce() PullOption

// WithConcurrency sets the number of concurrent chunk downloads.
// Values are clamped to the range [1, MaxConcurrency].
// Default is DefaultConcurrency (4).
func WithConcurrency(n int) PullOption

// WithProgress sets a callback for progress updates during download.
// The callback is invoked from download worker goroutines and must be thread-safe.
//
// The CLI uses this callback to display an enhanced progress bar with:
// - Download speed (KB/s or MB/s, auto-selected)
// - Elapsed time in human-readable format (e.g., "5s", "2m 30s")
// - Estimated time remaining in human-readable format
// - Byte-based progress tracking
// - Updates every second via ticker AND on each chunk completion
//
// Example: Downloading [============>                 ] 45% (5.2 MB/s, elapsed: 30s, remaining: 2m 15s)
func WithProgress(fn func(PullProgress)) PullOption

// ManagerOption configures a Manager.
type ManagerOption func(*managerConfig)

// managerConfig holds configuration for Manager construction.
type managerConfig struct {
	// httpClient is used for all HTTP requests to the registry.
	httpClient HTTPClient

	// logger receives diagnostic log messages.
	logger Logger
}

// WithHTTPClient sets a custom HTTP client for registry requests.
// Useful for testing with mock servers or customizing timeouts.
// If not set, http.DefaultClient is used.
func WithHTTPClient(client HTTPClient) ManagerOption

// WithLogger sets a logger for diagnostic output.
// If not set, logging is disabled.
func WithLogger(logger Logger) ManagerOption

// HTTPClient is the interface for HTTP operations.
// *http.Client satisfies this interface.
type HTTPClient interface {
	// Do sends an HTTP request and returns an HTTP response.
	Do(req *http.Request) (*http.Response, error)
}

// Logger is the interface for diagnostic logging.
// Compatible with slog, zap, logrus, and other structured loggers.
type Logger interface {
	// Debug logs a debug-level message with optional key-value pairs.
	Debug(msg string, keysAndValues ...any)

	// Info logs an info-level message with optional key-value pairs.
	Info(msg string, keysAndValues ...any)

	// Warn logs a warning-level message with optional key-value pairs.
	Warn(msg string, keysAndValues ...any)

	// Error logs an error-level message with optional key-value pairs.
	Error(msg string, keysAndValues ...any)
}
```

## 3. Public API (`models.go`)

```go
package models

import (
	"context"

	"github.com/spf13/cobra"
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

// NewManager creates a new Manager with the given configuration.
// Returns an error if the configuration is invalid (empty AppName or RegistryURL).
func NewManager(cfg Config, opts ...ManagerOption) (Manager, error)

// NewCommand creates a Cobra command tree for model management.
// The returned command should be added to a parent CLI's root command.
//
// Commands provided:
//   - models list [--remote]
//   - models pull <group/model> <version> [--force]
//   - models remove <group/model> <version> [--all] [-y|--yes]
//   - models info <group/model> <version> [--remote]
//   - models path <group/model> <version>
//   - models update [<group/model>] [--apply]
//   - models prune [-y|--yes]
//
// Global flags: --json, --quiet, --verbose
func NewCommand(cfg Config, opts ...ManagerOption) *cobra.Command
```

## 4. Internal Implementation

### 4.1 Manager Implementation (`manager.go`)

```go
package models

import (
	"context"
	"sync"
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
	// Uses storageInterface to enable test isolation.
	storage storageInterface

	// registry handles remote registry communication.
	registry *registryClient

	// downloadMu serializes pull operations within a single process.
	// Cross-process serialization uses file locking ({modelDir}/.pull.lock).
	downloadMu sync.Mutex
}

// Method signatures for manager (implements Manager interface):

func (m *manager) ListInstalled(ctx context.Context) ([]InstalledModel, error)

func (m *manager) ListRemote(ctx context.Context) ([]RemoteModel, error)

func (m *manager) ListRemoteGroup(ctx context.Context, group string) ([]RemoteModel, error)

func (m *manager) ListRemoteVersions(ctx context.Context, group, model string) ([]RemoteModel, error)

func (m *manager) GetInstalled(ctx context.Context, ref ModelRef) (InstalledModel, error)

func (m *manager) GetRemote(ctx context.Context, ref ModelRef) (RemoteModel, error)

func (m *manager) GetRemoteDetail(ctx context.Context, ref ModelRef) (RemoteModelDetail, error)

func (m *manager) Pull(ctx context.Context, ref ModelRef, opts ...PullOption) error

func (m *manager) Remove(ctx context.Context, ref ModelRef) error

func (m *manager) RemoveAll(ctx context.Context, group, model string) error

func (m *manager) Path(ctx context.Context, ref ModelRef) (string, error)

func (m *manager) CheckUpdate(ctx context.Context, ref ModelRef) (installedHash, remoteHash string, err error)

func (m *manager) PruneCache(ctx context.Context) error
```

### 4.2 Storage Layer (`storage.go`)

```go
package models

import (
	"sync"
	"time"
)

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
	// This enables offline inspection of installed model metadata.
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

// newStorage creates a new storage instance for the given configuration.
func newStorage(cfg Config) (*storage, error)

// loadRegistry reads and parses the local registry.json file.
// Returns an empty registry if the file doesn't exist.
func (s *storage) loadRegistry() (localRegistry, error)

// saveRegistry atomically writes the registry to registry.json.
// Uses cross-process file locking to prevent concurrent writes from multiple processes.
func (s *storage) saveRegistry(reg localRegistry) error

// modelPath returns the absolute path to a model's directory.
func (s *storage) modelPath(ref ModelRef) string

// chunkCachePath returns the path to the chunk cache directory.
func (s *storage) chunkCachePath() string

// ensureDir creates a directory and all parent directories if they don't exist.
func (s *storage) ensureDir(path string) error

// atomicWrite writes data to a file using write-then-rename for atomicity.
func (s *storage) atomicWrite(path string, data []byte) error

// removeModelDir removes a model's directory and all its contents.
func (s *storage) removeModelDir(ref ModelRef) error

// saveManifest stores the manifest in .{appName}/manifest.json within the model directory.
// Uses atomicWrite for safe file creation. Creates the metadata directory if needed.
// The manifest is stored after successful installation to enable:
//   - Offline inspection of installed model metadata
//   - Verification of installed files without network access
//   - Quick lookup of file list and sizes
func (s *storage) saveManifest(ref ModelRef, mf manifest) error

// envVarName constructs an environment variable name from the app name.
// Converts appName to uppercase and appends "_MODELS_DIR".
// Example: envVarName("xprim") returns "XPRIM_MODELS_DIR".
func envVarName(appName string) string
```

### 4.2.1 File Locking (`lock_unix.go`, `lock_windows.go`)

Cross-process file locking ensures that multiple processes (e.g., two terminal windows running `models pull` simultaneously) cannot corrupt `registry.json` by writing to it at the same time.

```go
package models

import "time"

// DefaultLockTimeout is the default timeout for acquiring file locks.
const DefaultLockTimeout = 30 * time.Second

// Locker provides mutual exclusion for file operations.
type Locker interface {
	// Lock acquires an exclusive lock on the file.
	// Blocks until lock is acquired or timeout expires.
	// Returns error if lock cannot be acquired within timeout.
	Lock() error

	// Unlock releases the lock.
	// Safe to call multiple times.
	Unlock() error
}

// fileLock implements Locker using platform-specific mechanisms:
// - Unix: flock() advisory locking (syscall.LOCK_EX|syscall.LOCK_NB)
// - Windows: LockFileEx() mandatory locking
type fileLock struct {
	// file is the lock file handle.
	file *os.File

	// timeout is the maximum duration to wait for lock acquisition.
	timeout time.Duration

	// locked tracks whether the lock is currently held.
	locked bool
}

// newFileLock creates a new file lock for the given path.
// Creates the lock file if it doesn't exist.
func newFileLock(path string, timeout time.Duration) (*fileLock, error)

// Lock acquires an exclusive lock using polling with exponential backoff.
// Initial sleep: 10ms, max: 100ms, until timeout expires.
func (l *fileLock) Lock() error

// Unlock releases the lock and closes the file handle.
// Safe to call multiple times.
func (l *fileLock) Unlock() error
```

**Lock File Pattern:**
- Lock file location: `registry.json.lock` (same directory as `registry.json`)
- Lock is acquired BEFORE writing to `registry.json`
- Lock is held during the entire write operation
- Deferred unlock ensures release even on error

### 4.3 Platform-Specific Storage Paths

#### macOS (`storage_darwin.go`)

```go
//go:build darwin
// +build darwin

package models

// getDefaultDataDir returns the default data directory for macOS.
// Returns ~/Library/Application Support/<appName>/models/
func getDefaultDataDir(appName string) (string, error)
```

#### Linux (`storage_linux.go`)

```go
//go:build linux
// +build linux

package models

// getDefaultDataDir returns the default data directory for Linux.
// Uses $XDG_DATA_HOME/<appName>/models/ if set,
// otherwise ~/.local/share/<appName>/models/
func getDefaultDataDir(appName string) (string, error)
```

#### Windows (`storage_windows.go`)

```go
//go:build windows
// +build windows

package models

// getDefaultDataDir returns the default data directory for Windows.
// Returns %APPDATA%\<appName>\models\
func getDefaultDataDir(appName string) (string, error)
```

### 4.4 Registry Client (`registry.go`)

```go
package models

import "context"

// modelsIndex represents the models.json file from the remote registry.
// Structure: group → model → version → versionEntry
// Example: index["fast-whisper"]["tiny"]["v1.0.0"] returns the versionEntry
type modelsIndex map[string]map[string]map[string]versionEntry

// versionEntry represents a version in the remote registry.
type versionEntry struct {
	// Hash is the SHA-256 hash of the manifest for this version.
	Hash string `json:"hash"`

	// Metadata contains optional arbitrary metadata for this version.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// manifest represents the parsed manifest file from data/<hash>.
type manifest struct {
	// TotalSize is the total size of all files in bytes.
	TotalSize int64 `json:"total_size"`

	// ChunkSize is the size of each chunk in bytes.
	ChunkSize int64 `json:"chunk_size"`

	// Chunks is the ordered list of chunk hashes (SHA-256).
	Chunks []string `json:"chunks"`

	// Files lists all files in the model with their paths and sizes.
	Files []manifestFile `json:"files"`
}

// manifestFile represents a file entry in a manifest.
type manifestFile struct {
	// Path is the relative path within the model directory.
	Path string `json:"path"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`
}

// registryClient handles HTTP communication with the remote model registry.
type registryClient struct {
	// baseURL is the base URL of the registry (e.g., "https://pub-abc123.r2.dev").
	baseURL string

	// httpClient is used for HTTP requests.
	httpClient HTTPClient

	// logger receives diagnostic messages. May be nil.
	logger Logger
}

// newRegistryClient creates a new registry client.
func newRegistryClient(baseURL string, client HTTPClient, logger Logger) *registryClient

// fetchModelsIndex fetches and parses the models.json index from the registry.
func (r *registryClient) fetchModelsIndex(ctx context.Context) (modelsIndex, error)

// fetchManifest fetches and parses a manifest from data/<hash>.
func (r *registryClient) fetchManifest(ctx context.Context, hash string) (manifest, error)

// fetchChunk fetches a chunk from data/<hash> and returns its contents.
// The caller is responsible for verifying the hash.
func (r *registryClient) fetchChunk(ctx context.Context, hash string) ([]byte, error)

// fetchChunkWithProgress fetches a chunk with optional progress reporting.
// The onProgress callback is called as bytes are read from the network.
// It receives the delta (bytes just read), not cumulative.
// Used by downloadEngine to track bytesInProgress for smooth speed calculation.
func (r *registryClient) fetchChunkWithProgress(ctx context.Context, hash string, onProgress func(delta int64)) ([]byte, error)
```

### 4.5 Download Engine (`download.go`)

```go
package models

import (
	"context"
	"sync"
	"time"
)

// ChunkCacheMaxAge is the maximum age for cached chunks before cleanup.
const ChunkCacheMaxAge = 24 * time.Hour

// chunkJob represents a unit of work for the download worker pool.
type chunkJob struct {
	// hash is the SHA-256 hash of the chunk to download.
	hash string

	// index is the position of this chunk in the sequence.
	index int

	// size is the expected size of the chunk in bytes.
	size int64
}

// downloadResult contains the result of a chunk download operation.
type downloadResult struct {
	// index identifies which chunk this result is for.
	index int

	// err is nil on success, or the error that occurred.
	err error
}

// verifyHash computes the SHA-256 hash of data and compares it to expectedHash.
// Returns nil if the hash matches, ErrHashMismatch if verification fails.
// The expectedHash should be a lowercase hex-encoded string.
// Used by registryClient.fetchChunk and fetchManifest to verify integrity.
func verifyHash(data []byte, expectedHash string) error

// downloadEngine orchestrates the parallel download of chunks.
type downloadEngine struct {
	// registry is used to fetch chunks from the remote registry.
	registry *registryClient

	// storage provides access to the chunk cache.
	storage storageInterface

	// logger receives diagnostic messages. May be nil.
	logger Logger

	// errChan receives the first fatal error from workers.
	// Buffered with capacity 1 to prevent goroutine leaks.
	errChan chan error

	// wg tracks active download workers for graceful shutdown.
	wg sync.WaitGroup

	// bytesInProgress tracks bytes currently being downloaded across all workers.
	// Updated atomically as bytes are read from the network.
	bytesInProgress int64
}

// newDownloadEngine creates a new download engine.
func newDownloadEngine(registry *registryClient, storage storageInterface, logger Logger) *downloadEngine

// downloadChunks downloads all specified chunks with parallel workers.
// The progressFn is called after each chunk completes with (completed, total, bytesDownloaded, bytesInProgress).
// bytesDownloaded is the cumulative bytes fetched from network (excludes cache hits).
// bytesInProgress is bytes currently being read across all workers (for smooth speed calc).
// Chunks are cached to avoid re-downloading on retry or partial failure.
// Uses errChan and wg for concurrent coordination.
func (d *downloadEngine) downloadChunks(ctx context.Context, chunks []string, concurrency int, progressFn func(completed, total int, bytesDownloaded, bytesInProgress int64)) error

// chunkCache manages temporary storage of downloaded chunks.
type chunkCache struct {
	// cacheDir is the directory where chunks are stored.
	cacheDir string

	// mu protects cache operations.
	mu sync.RWMutex
}

// newChunkCache creates a new chunk cache in the specified directory.
func newChunkCache(cacheDir string) *chunkCache

// get retrieves a cached chunk by hash.
// Returns the chunk data and true if found, nil and false if not cached.
func (c *chunkCache) get(hash string) ([]byte, bool)

// put stores a chunk in the cache.
// The chunk is stored with its hash as the filename.
func (c *chunkCache) put(hash string, data []byte) error

// delete removes a chunk from the cache.
func (c *chunkCache) delete(hash string) error

// cleanup removes all cached chunks older than maxAge.
func (c *chunkCache) cleanup(maxAge time.Duration) error
```

### 4.6 File Reconstructor (`reconstruct.go`)

```go
package models

import (
	"context"
	"io"
)

// fileReconstructor assembles files from cached chunks.
type fileReconstructor struct {
	// chunkCache provides access to downloaded chunks.
	chunkCache *chunkCache

	// storage is used to write the reconstructed files.
	storage storageInterface
}

// newFileReconstructor creates a new file reconstructor.
func newFileReconstructor(cache *chunkCache, storage storageInterface) *fileReconstructor

// reconstruct extracts all files from the manifest using cached chunks.
// The chunks are read in order and streamed to the appropriate output files.
// The progressFn is called with the current file being processed.
func (f *fileReconstructor) reconstruct(ctx context.Context, mf manifest, ref ModelRef, progressFn func(currentFile string)) error

// chunkStreamReader provides an io.Reader interface over a sequence of chunks.
// This allows files to be written without loading all chunks into memory.
// Chunks are deleted incrementally as they are fully consumed to minimize disk space usage.
type chunkStreamReader struct {
	// chunks is the ordered list of chunk hashes.
	chunks []string

	// cache provides access to chunk data.
	cache *chunkCache

	// currentChunk is the index of the current chunk being read.
	currentChunk int

	// currentData is the remaining data in the current chunk.
	currentData []byte

	// totalRead tracks bytes read across all chunks.
	totalRead int64

	// lastDeletedChunk tracks which chunks have been deleted.
	// All chunks with index < lastDeletedChunk have been deleted.
	lastDeletedChunk int
}

// newChunkStreamReader creates a reader that streams through the given chunks.
func newChunkStreamReader(chunks []string, cache *chunkCache) *chunkStreamReader

// Read implements io.Reader, reading sequentially through all chunks.
// Chunks are deleted incrementally as they are fully consumed.
func (r *chunkStreamReader) Read(p []byte) (n int, err error)

// deleteRemainingChunks cleans up any chunks not yet deleted.
// Call this after reconstruction completes successfully.
func (r *chunkStreamReader) deleteRemainingChunks()

// Ensure chunkStreamReader implements io.Reader.
var _ io.Reader = (*chunkStreamReader)(nil)
```

**Incremental Chunk Deletion:**
- When a new chunk is loaded, the previous chunk (fully consumed) is deleted
- This reduces disk space from ~2x model size to ~1x model size + 1 chunk
- On reconstruction failure, unconsumed chunks remain for resume capability

## 5. CLI (`cmd/xprim-models/main.go`)

### 5.1 Exit Codes

```go
package main

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
```

### 5.2 CLI Structure

```go
package main

import (
	"os"

	"github.com/prethora/xprim/models"
)

// main is the entry point for the CLI test harness.
// Configuration is loaded from environment variables:
//   - XPRIM_REGISTRY_URL: Base URL of the model registry
//   - XPRIM_MODELS_DIR: Override for data directory (optional)
//
// The CLI uses NewCommand to create the command tree and handles
// os.Exit with appropriate exit codes based on error types.
func main()

// exitCodeFromError maps error types to exit codes.
func exitCodeFromError(err error) int
```

## 6. Test Scaffolding

### 6.1 Mock HTTP Client (`mock_test.go`)

```go
package models

import (
	"net/http"
	"sync"
)

// mockHTTPClient is a test double for HTTPClient.
type mockHTTPClient struct {
	// mu protects concurrent access to fields.
	mu sync.Mutex

	// responses maps URL paths to pre-configured responses.
	responses map[string]*http.Response

	// errors maps URL paths to pre-configured errors.
	errors map[string]error

	// calls records all URLs that were requested.
	calls []string
}

// newMockHTTPClient creates a new mock HTTP client.
func newMockHTTPClient() *mockHTTPClient

// Do implements HTTPClient, returning pre-configured responses or errors.
func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error)

// SetResponse configures a response for the given URL path.
func (m *mockHTTPClient) SetResponse(path string, resp *http.Response)

// SetError configures an error for the given URL path.
func (m *mockHTTPClient) SetError(path string, err error)

// Calls returns a copy of all URLs that were requested.
func (m *mockHTTPClient) Calls() []string

// Reset clears all configured responses, errors, and recorded calls.
func (m *mockHTTPClient) Reset()
```

### 6.2 Mock Logger (`mock_test.go`)

```go
package models

import "sync"

// logEntry represents a single log message.
type logEntry struct {
	// level is the log level: "debug", "info", "warn", or "error".
	level string

	// msg is the log message.
	msg string

	// keysAndValues contains the structured key-value pairs.
	keysAndValues []any
}

// mockLogger is a test double for Logger that captures all log messages.
type mockLogger struct {
	// mu protects concurrent access to entries.
	mu sync.Mutex

	// entries contains all logged messages.
	entries []logEntry
}

// newMockLogger creates a new mock logger.
func newMockLogger() *mockLogger

// Debug implements Logger.
func (m *mockLogger) Debug(msg string, keysAndValues ...any)

// Info implements Logger.
func (m *mockLogger) Info(msg string, keysAndValues ...any)

// Warn implements Logger.
func (m *mockLogger) Warn(msg string, keysAndValues ...any)

// Error implements Logger.
func (m *mockLogger) Error(msg string, keysAndValues ...any)

// Entries returns a copy of all logged entries.
func (m *mockLogger) Entries() []logEntry

// EntriesAtLevel returns all entries at the specified level.
func (m *mockLogger) EntriesAtLevel(level string) []logEntry

// Reset clears all recorded entries.
func (m *mockLogger) Reset()

// Ensure mockLogger implements Logger.
var _ Logger = (*mockLogger)(nil)
```

### 6.3 Mock Storage (`mock_test.go`)

```go
package models

import "sync"

// mockStorage is a test double for storageInterface.
// Provides in-memory storage for testing without filesystem dependencies.
type mockStorage struct {
	// mu protects concurrent access to fields.
	mu sync.Mutex

	// registry holds the in-memory local registry.
	registry localRegistry

	// models maps ModelRef.String() to model data.
	models map[string][]byte

	// chunks maps hash to chunk data.
	chunks map[string][]byte

	// loadRegistryErr is returned by loadRegistry if set.
	loadRegistryErr error

	// saveRegistryErr is returned by saveRegistry if set.
	saveRegistryErr error
}

// newMockStorage creates a new mock storage with empty registry.
func newMockStorage() *mockStorage

// loadRegistry implements storageInterface.
func (m *mockStorage) loadRegistry() (localRegistry, error)

// saveRegistry implements storageInterface.
func (m *mockStorage) saveRegistry(reg localRegistry) error

// modelPath implements storageInterface.
func (m *mockStorage) modelPath(ref ModelRef) string

// chunkCachePath implements storageInterface.
func (m *mockStorage) chunkCachePath() string

// ensureDir implements storageInterface.
func (m *mockStorage) ensureDir(path string) error

// atomicWrite implements storageInterface.
func (m *mockStorage) atomicWrite(path string, data []byte) error

// removeModelDir implements storageInterface.
func (m *mockStorage) removeModelDir(ref ModelRef) error

// saveManifest implements storageInterface.
func (m *mockStorage) saveManifest(ref ModelRef, mf manifest) error

// SetLoadRegistryError configures an error to be returned by loadRegistry.
func (m *mockStorage) SetLoadRegistryError(err error)

// SetSaveRegistryError configures an error to be returned by saveRegistry.
func (m *mockStorage) SetSaveRegistryError(err error)

// Ensure mockStorage implements storageInterface.
var _ storageInterface = (*mockStorage)(nil)
```
