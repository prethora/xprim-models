# Client Need: xprim-models Module

## Overview

A Golang module for consuming models from an XPRIM content-addressed model registry. The module downloads, caches, and manages ML models stored on remote registries, providing a clean API for model retrieval and local storage management.

**This module is designed as a reusable package** that exposes a clean API for model management. The CLI tool included in this project (`xprim-models`) is a test harness that consumes the module's public API—the same API that other CLI tools will use when they import this module to provide model management capabilities.

The module is intended to be embedded into larger CLI applications. For example, a speech recognition CLI might import this module to provide `mytool models pull ...` commands alongside its core functionality.

## Platform Support

### Target Platforms

**Primary Platforms:**
- **macOS** (arm64, amd64)
- **Linux** (amd64, arm64)
- **Windows** (amd64)

**Rationale**: ML models are consumed across all major desktop platforms. Users may run inference tools on any of these systems.

**Platform-Specific Considerations**:
- **Storage paths**: Must use platform-appropriate directories (XDG on Linux, Application Support on macOS, AppData on Windows)
- **Symlinks**: Not used for alias handling due to Windows limitations

## Architecture

### Module Design Philosophy

The module must be implemented as a **standalone, importable Go package** with a well-defined public API. The package should:

- Accept configuration (app name, registry URL) at construction time
- Manage local model storage in platform-appropriate directories
- Download and verify models using content-addressed hashing
- Cache downloaded chunks for resume capability
- Expose commands via Cobra for easy integration into parent CLIs
- Be fully testable with mock HTTP clients

### Integration Pattern

The module exposes a Cobra command that parent CLIs attach to their command tree:

```go
// In parent CLI
import "github.com/prethora/xprim-models/models"

func main() {
    rootCmd := &cobra.Command{Use: "mytool"}
    
    // Attach the models subcommand
    modelsCmd := models.NewCommand(models.Config{
        AppName:     "mytool",
        RegistryURL: "https://pub-abc123.r2.dev",
    })
    rootCmd.AddCommand(modelsCmd)
    
    rootCmd.Execute()
}
```

This results in commands like:
```
mytool models list
mytool models pull fast-whisper/tiny fp16
mytool models remove fast-whisper/tiny fp16
```

## Public API Surface

```go
package models

import (
    "context"
    "io"
    "time"
    
    "github.com/spf13/cobra"
)

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
    Group   string // e.g., "fast-whisper"
    Model   string // e.g., "tiny"
    Version string // e.g., "fp16", "int8", "q4"
}

// String returns the canonical string form: group/model version
func (r ModelRef) String() string

// ParseModelRef parses "group/model" or "group/model version" into a ModelRef.
// Returns error if format is invalid.
func ParseModelRef(s string) (ModelRef, error)

// InstalledModel contains information about a locally installed model.
type InstalledModel struct {
    Ref          ModelRef
    ManifestHash string    // SHA-256 of the manifest
    TotalSize    int64     // Total size in bytes
    FileCount    int       // Number of files in the model
    InstalledAt  time.Time // When the model was installed
    Path         string    // Absolute path to model directory
}

// RemoteModel contains information about a model available in the registry.
type RemoteModel struct {
    Ref          ModelRef
    ManifestHash string            // SHA-256 of the manifest
    AliasOf      string            // If this is an alias, the target version
    Metadata     map[string]any    // Optional metadata from registry
}

// RemoteModelDetail contains detailed information from a model's manifest.
type RemoteModelDetail struct {
    RemoteModel
    TotalSize  int64      // Total size in bytes
    ChunkSize  int64      // Size of each chunk
    ChunkCount int        // Number of chunks
    Files      []FileInfo // Files contained in the model
}

// FileInfo describes a file within a model.
type FileInfo struct {
    Path string // Relative path within model directory
    Size int64  // Size in bytes
}

// PullProgress reports download progress during a pull operation.
type PullProgress struct {
    Phase           string  // "manifest", "chunks", "extracting"
    ChunksTotal     int     // Total chunks to download
    ChunksCompleted int     // Chunks downloaded so far
    BytesTotal      int64   // Total bytes to download
    BytesCompleted  int64   // Bytes downloaded so far
    CurrentFile     string  // Current file being processed (during extraction)
}

// PullOption configures a pull operation.
type PullOption func(*pullConfig)

// WithForce forces re-download even if model is already installed.
func WithForce() PullOption

// WithConcurrency sets the number of concurrent chunk downloads.
// Default: 4, Max: 16
func WithConcurrency(n int) PullOption

// WithProgress sets a callback for progress updates.
func WithProgress(fn func(PullProgress)) PullOption

// Manager provides programmatic access to model management.
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
    GetRemoteDetail(ctx context.Context, ref ModelRef) (RemoteModelDetail, error)
    
    // Pull downloads and installs a model from the registry.
    // If the model is already installed with the same manifest hash, returns immediately
    // unless WithForce() is specified.
    Pull(ctx context.Context, ref ModelRef, opts ...PullOption) error
    
    // Remove deletes a locally installed model.
    // Returns ErrNotInstalled if the model is not installed.
    Remove(ctx context.Context, ref ModelRef) error
    
    // RemoveAll deletes all versions of a model.
    RemoveAll(ctx context.Context, group, model string) error
    
    // Path returns the absolute path to an installed model's directory.
    // Returns ErrNotInstalled if the model is not installed.
    Path(ctx context.Context, ref ModelRef) (string, error)
    
    // CheckUpdate checks if a newer version is available for an alias.
    // Returns the current installed hash and the remote hash.
    // If they differ, an update is available.
    CheckUpdate(ctx context.Context, ref ModelRef) (installedHash, remoteHash string, err error)
}

// ManagerOption configures a Manager.
type ManagerOption func(*managerConfig)

// WithHTTPClient sets a custom HTTP client for registry requests.
// Useful for testing with mock servers.
func WithHTTPClient(client HTTPClient) ManagerOption

// WithLogger sets a logger for diagnostic output.
func WithLogger(logger Logger) ManagerOption

// HTTPClient interface for HTTP operations.
// *http.Client satisfies this interface.
type HTTPClient interface {
    Do(req *http.Request) (*http.Response, error)
}

// Logger interface for diagnostic output.
type Logger interface {
    Debug(msg string, keysAndValues ...any)
    Info(msg string, keysAndValues ...any)
    Warn(msg string, keysAndValues ...any)
    Error(msg string, keysAndValues ...any)
}

// NewManager creates a new Manager with the given configuration.
func NewManager(cfg Config, opts ...ManagerOption) (Manager, error)

// NewCommand creates a Cobra command tree for model management.
// The returned command should be added to a parent command.
//
// Commands provided:
//   models list [--remote]
//   models pull <group/model> <version> [--force]
//   models remove <group/model> <version> [--all]
//   models info <group/model> <version> [--remote]
//   models path <group/model> <version>
//   models update [<group/model>] [--apply]
func NewCommand(cfg Config, opts ...ManagerOption) *cobra.Command
```

## Consumer Usage Pattern

### Embedding in a Parent CLI

```go
package main

import (
    "os"
    
    "github.com/spf13/cobra"
    "github.com/prethora/xprim-models/models"
)

func main() {
    rootCmd := &cobra.Command{
        Use:   "whisper-tool",
        Short: "Speech recognition tool",
    }
    
    // Add the models subcommand
    modelsCmd := models.NewCommand(models.Config{
        AppName:     "whisper-tool",
        RegistryURL: "https://pub-abc123.r2.dev",
    })
    rootCmd.AddCommand(modelsCmd)
    
    // Add other commands...
    rootCmd.AddCommand(transcribeCmd)
    
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

### Programmatic Usage

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/prethora/xprim-models/models"
)

func main() {
    ctx := context.Background()
    
    mgr, err := models.NewManager(models.Config{
        AppName:     "my-app",
        RegistryURL: "https://pub-abc123.r2.dev",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Pull a model with progress reporting
    err = mgr.Pull(ctx, models.ModelRef{
        Group:   "fast-whisper",
        Model:   "tiny",
        Version: "fp16",
    }, models.WithProgress(func(p models.PullProgress) {
        fmt.Printf("\r%s: %d/%d chunks", p.Phase, p.ChunksCompleted, p.ChunksTotal)
    }))
    if err != nil {
        log.Fatal(err)
    }
    
    // Get the path to use the model
    path, err := mgr.Path(ctx, models.ModelRef{
        Group:   "fast-whisper",
        Model:   "tiny",
        Version: "fp16",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("\nModel installed at: %s\n", path)
}
```

## CLI Command Interface

### `models list`

Lists locally installed models by default.

```
$ mytool models list
GROUP          MODEL    VERSION    SIZE       INSTALLED
fast-whisper   tiny     fp16       75.5 MB    2024-01-15
fast-whisper   tiny     int8       39.2 MB    2024-01-14
fast-whisper   large    fp16       1.5 GB     2024-01-10

$ mytool models list --remote
GROUP          MODEL    VERSION    SIZE
fast-whisper   tiny     fp16       75.5 MB
fast-whisper   tiny     int8       39.2 MB
fast-whisper   tiny     q4         22.1 MB
fast-whisper   large    fp16       1.5 GB
llama          7b       q4         3.8 GB
```

### `models pull <group/model> <version>`

Downloads a model from the registry. Version is required.

```
$ mytool models pull fast-whisper/tiny fp16
Fetching manifest... done
Downloading: 8/8 chunks [====================] 100% 75.5 MB
Extracting files... done

Model installed: fast-whisper/tiny fp16 (75.5 MB)
Path: /home/user/.local/share/mytool/models/fast-whisper/tiny/fp16

$ mytool models pull fast-whisper/tiny fp16
Model already installed: fast-whisper/tiny fp16
Use --force to re-download

$ mytool models pull fast-whisper/tiny fp16 --force
Fetching manifest... done
Downloading: 8/8 chunks [====================] 100% 75.5 MB
Extracting files... done

Model installed: fast-whisper/tiny fp16 (75.5 MB)
```

### `models remove <group/model> <version>`

Removes a locally installed model.

```
$ mytool models remove fast-whisper/tiny fp16
Removed: fast-whisper/tiny fp16

$ mytool models remove fast-whisper/tiny --all
Removed: fast-whisper/tiny fp16
Removed: fast-whisper/tiny int8
Removed 2 versions
```

### `models info <group/model> <version>`

Shows detailed information about a model.

```
$ mytool models info fast-whisper/tiny fp16
Model:        fast-whisper/tiny fp16
Status:       Installed
Size:         75.5 MB
Files:        3
Installed:    2024-01-15 10:30:00
Path:         /home/user/.local/share/mytool/models/fast-whisper/tiny/fp16

Files:
  model.safetensors    74.2 MB
  config.json          1.2 KB
  tokenizer.json       1.3 MB

$ mytool models info fast-whisper/tiny fp16 --remote
Model:        fast-whisper/tiny fp16
Status:       Available (not installed)
Size:         75.5 MB
Chunks:       8 × 10 MB
Files:        3

Files:
  model.safetensors    74.2 MB
  config.json          1.2 KB
  tokenizer.json       1.3 MB

Metadata:
  framework: onnx
  quantization: fp16
```

### `models path <group/model> <version>`

Outputs the path to an installed model. Useful for scripts.

```
$ mytool models path fast-whisper/tiny fp16
/home/user/.local/share/mytool/models/fast-whisper/tiny/fp16

$ mytool models path fast-whisper/tiny fp16 || echo "Not installed"
error: model not installed: fast-whisper/tiny fp16
Not installed
```

### `models update [<group/model>] [--apply]`

Checks for updates to installed aliases.

```
$ mytool models update
Checking for updates...

fast-whisper/tiny: up to date
llama/7b: update available (q4_v2 → q4_v3)

$ mytool models update llama/7b --apply
Downloading update for llama/7b...
Downloading: 380/380 chunks [====================] 100% 3.8 GB
Extracting files... done

Updated: llama/7b (3.8 GB)
```

### Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output as JSON for scripting |
| `--quiet` | Suppress progress output |
| `--verbose` | Enable verbose/debug output |

## Core Functionality

### Download Process

```
    Pull(ref)
        │
        ▼
    Fetch models.json from registry
        │
        ▼
    Resolve version (check if alias → get target)
        │
        ▼
    Check if already installed (compare manifest hash)
        │
        ├── Same hash, no --force → Return (already installed)
        │
        ▼
    Fetch manifest from data/<manifest-hash>
        │
        ▼
    For each chunk in manifest.chunks:
        │
        ├── Check chunk cache
        │   ├── Cached and valid → Skip download
        │   └── Not cached → Download from data/<chunk-hash>
        │                     Verify SHA-256
        │                     Store in chunk cache
        │
        ▼
    Reconstruct files from chunks:
        │
        ├── Concatenate all chunks in memory/streaming
        ├── For each file in manifest.files:
        │   └── Extract size bytes → Write to path
        │
        ▼
    Update local registry.json
        │
        ▼
    Clean up chunk cache for this model
```

### Chunk Caching

Downloaded chunks are cached temporarily to enable resume on interrupted downloads:

```
<data-dir>/cache/
└── chunks/
    ├── a1b2c3d4e5f6...    # Cached chunk (named by hash)
    ├── b2c3d4e5f6a1...
    └── ...
```

Cache behavior:
- Chunks are stored by hash during download
- On successful model installation, chunks for that model are removed
- On failure, chunks remain for resume
- Periodic cleanup removes chunks older than 24 hours

### File Reconstruction

Files are extracted by reading sequentially through the concatenated chunks:

1. Open all chunks as a single logical stream
2. For each file in manifest.files (in order):
   - Read exactly `file.size` bytes
   - Create directories as needed
   - Write to `<model-dir>/<file.path>`
3. Verify total bytes read equals `manifest.total_size`

## Storage Structure

### Platform-Specific Base Directories

| Platform | Base Path |
|----------|-----------|
| Linux | `$XDG_DATA_HOME/<app>/models/` or `~/.local/share/<app>/models/` |
| macOS | `~/Library/Application Support/<app>/models/` |
| Windows | `%APPDATA%\<app>\models\` |

### Directory Layout

```
<base>/
├── registry.json                      # Local index of installed models
├── cache/
│   └── chunks/                        # Temporary chunk cache
│       ├── a1b2c3...
│       └── b2c3d4...
└── fast-whisper/                      # Group directory
    └── tiny/                          # Model directory
        └── fp16/                      # Version directory
            ├── .<app>/                # Hidden metadata directory
            │   └── manifest.json      # Copy of manifest
            ├── model.safetensors      # Model files
            ├── config.json
            └── tokenizer.json
```

### Local Registry (registry.json)

Tracks installed models:

```json
{
  "fast-whisper": {
    "tiny": {
      "fp16": {
        "manifest_hash": "a1b2c3d4e5f6789012345678901234567890123456789012345678901234abcd",
        "total_size": 79167488,
        "file_count": 3,
        "installed_at": "2024-01-15T10:30:00Z"
      },
      "int8": {
        "manifest_hash": "b2c3d4e5f6789012345678901234567890123456789012345678901234abcdef",
        "total_size": 41104384,
        "file_count": 3,
        "installed_at": "2024-01-14T14:00:00Z"
      }
    }
  }
}
```

## Error Handling

### Sentinel Errors

```go
package models

import "errors"

var (
    // ErrModelNotFound indicates the model does not exist in the registry.
    ErrModelNotFound = errors.New("models: model not found in registry")
    
    // ErrVersionNotFound indicates the version does not exist for the model.
    ErrVersionNotFound = errors.New("models: version not found")
    
    // ErrNotInstalled indicates the model is not installed locally.
    ErrNotInstalled = errors.New("models: model not installed")
    
    // ErrAlreadyInstalled indicates the model is already installed.
    // Returned by Pull when model exists and --force not specified.
    ErrAlreadyInstalled = errors.New("models: model already installed")
    
    // ErrHashMismatch indicates downloaded data failed hash verification.
    ErrHashMismatch = errors.New("models: hash verification failed")
    
    // ErrNetworkError indicates a network or connection error.
    ErrNetworkError = errors.New("models: network error")
    
    // ErrStorageError indicates a filesystem operation failed.
    ErrStorageError = errors.New("models: storage error")
    
    // ErrInvalidRef indicates an invalid model reference format.
    ErrInvalidRef = errors.New("models: invalid model reference")
    
    // ErrRegistryError indicates the registry returned invalid data.
    ErrRegistryError = errors.New("models: invalid registry response")
)
```

### Exit Codes (CLI)

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Model not found (registry) |
| 4 | Model not installed (local) |
| 5 | Network error |
| 6 | Hash verification failed |
| 7 | Storage error |

## Thread Safety

- **Manager is thread-safe** — All Manager methods can be called concurrently
- **Progress callbacks** — Called from download goroutines; callback must be thread-safe
- **Concurrent downloads** — Multiple chunks download in parallel (configurable)

## CLI Test Interface

The module includes a standalone CLI for testing:

```
xprim-models --registry <url> list [--remote]
xprim-models --registry <url> pull <group/model> <version> [--force]
xprim-models --registry <url> remove <group/model> <version> [--all]
xprim-models --registry <url> info <group/model> <version> [--remote]
xprim-models --registry <url> path <group/model> <version>
xprim-models --registry <url> update [<group/model>] [--apply]
```

The registry URL can also be set via `XPRIM_MODELS_REGISTRY_URL` environment variable.

**The CLI must not access any internal/unexported functionality**—if the CLI needs something to work, it must be exposed in the public API.

## Configuration via Environment

| Variable | Description |
|----------|-------------|
| `<APPNAME>_MODELS_DIR` | Override the models storage directory |
| `XPRIM_MODELS_REGISTRY_URL` | Registry URL for test CLI |

Where `<APPNAME>` is the uppercase form of Config.AppName (e.g., `MYTOOL_MODELS_DIR`).

## Code Quality Expectations

| Component | Quality Level | Rationale |
|-----------|---------------|-----------|
| Public API design | Production-ready | Contract for all consumers |
| Download/verification | Production-ready | Data integrity critical |
| Chunk caching | Production-ready | Resume capability important |
| Storage layer | Production-ready | Data integrity matters |
| File reconstruction | Production-ready | Must handle large files correctly |
| Progress reporting | Production-ready | User experience |
| CLI scaffolding | Functional | Test harness |

## Out of Scope

- Model upload/push (that's the admin CLI)
- Registry management
- Authentication (registry is public)
- Model execution/inference
- Custom domains or registry discovery
- Garbage collection of orphaned data
- Model deletion from registry

## Success Criteria

1. **Cross-platform storage**: Correctly uses platform-appropriate directories on macOS, Linux, and Windows
2. **Content verification**: All downloads verified against SHA-256 hashes
3. **Resume capability**: Interrupted downloads resume from cached chunks
4. **Deduplication**: Chunks shared across models are only downloaded once
5. **Progress reporting**: Clear, accurate progress during downloads
6. **CLI integration**: Seamlessly embeds into parent CLI tools via Cobra
7. **Programmatic API**: Clean Manager interface for non-CLI usage
8. **Error clarity**: All errors are typed and actionable
9. **Concurrent downloads**: Parallel chunk downloads with configurable concurrency
10. **Thread safety**: Safe for concurrent use from multiple goroutines
11. **API validation**: Test CLI operates using only public API

## Dependencies

### External Dependencies

```
module github.com/prethora/xprim-models

go 1.25

require (
    github.com/spf13/cobra v1.8.0
    github.com/schollz/progressbar/v3 v3.17.0
)
```

### Standard Library Usage

| Package | Usage |
|---------|-------|
| `context` | Request cancellation, timeouts |
| `crypto/sha256` | Content verification |
| `encoding/json` | Registry and manifest parsing |
| `errors` | Sentinel errors, wrapping |
| `fmt` | Formatted output |
| `io` | Reader interfaces, streaming |
| `net/http` | Registry downloads |
| `os` | File operations, environment |
| `path/filepath` | Cross-platform paths |
| `runtime` | Platform detection (GOOS) |
| `sync` | Concurrent download coordination |
| `time` | Timestamps, cache expiry |