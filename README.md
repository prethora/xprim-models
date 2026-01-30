# xprim-models

A Go module for consuming ML models from an XPRIM content-addressed model registry.

## Features

- Download models from a content-addressed registry with SHA-256 verification
- Chunked downloads with parallel workers for fast transfers
- Resumable downloads (cached chunks survive interruptions)
- Progress reporting with real-time speed, elapsed time, and ETA
- Platform-appropriate storage paths (macOS, Linux, Windows)
- Both CLI and programmatic APIs

## Installation

```bash
go get github.com/prethora/xprim-models
```

## Configuration

The module requires two configuration values:

- **AppName**: Determines the storage directory name (e.g., "xprim" → `~/.local/share/xprim/models/` on Linux)
- **RegistryURL**: Base URL of the model registry (e.g., "https://registry.example.com")

Optional:
- **DataDir**: Override the default data directory (can also be set via `<APPNAME>_MODELS_DIR` environment variable)

## CLI Usage

### Embedding in Your CLI

```go
package main

import (
    "os"
    "github.com/prethora/xprim-models"
    "github.com/spf13/cobra"
)

func main() {
    rootCmd := &cobra.Command{Use: "myapp"}

    // Add the models command tree
    modelsCmd := models.NewCommand(models.Config{
        AppName:     "myapp",
        RegistryURL: os.Getenv("MYAPP_REGISTRY_URL"),
    })
    rootCmd.AddCommand(modelsCmd)

    rootCmd.Execute()
}
```

### Available Commands

```
models list [--remote]              List installed or remote models
models pull <group/model> <version> Download and install a model
models remove <group/model> <version> [--all] [-y]  Remove installed model(s)
models info <group/model> <version> [--remote]      Show model details
models path <group/model> <version> Output path to installed model
models update [<group/model>] [--apply]             Check for or apply updates
models prune [-y]                   Clear the chunk cache
```

**Global Flags:**
- `--json`: Output in JSON format
- `--quiet`: Suppress non-essential output
- `--verbose`: Show detailed output

### Examples

```bash
# List available models from registry
myapp models list --remote

# Download a model
myapp models pull whisper/ggml-medium q8_0

# Get path to installed model (for use in scripts)
MODEL_PATH=$(myapp models path whisper/ggml-medium q8_0)

# Check for updates
myapp models update

# Remove a model (with confirmation)
myapp models remove whisper/ggml-medium q8_0

# Remove without confirmation (for scripts)
myapp models remove whisper/ggml-medium q8_0 -y

# Clear download cache
myapp models prune -y
```

## Programmatic API

For applications that need to manage models programmatically (auto-downloading, checking installed models, etc.), use the `Manager` interface directly.

### Creating a Manager

```go
import "github.com/prethora/xprim-models"

mgr, err := models.NewManager(models.Config{
    AppName:     "myapp",
    RegistryURL: "https://registry.example.com",
})
if err != nil {
    log.Fatal(err)
}
```

### Listing Models

```go
// List installed models
installed, err := mgr.ListInstalled(ctx)
for _, m := range installed {
    fmt.Printf("%s/%s %s - %s\n", m.Ref.Group, m.Ref.Model, m.Ref.Version, m.Path)
}

// List remote models from registry
remote, err := mgr.ListRemote(ctx)
for _, m := range remote {
    fmt.Printf("%s/%s %s\n", m.Ref.Group, m.Ref.Model, m.Ref.Version)
}

// List models in a specific group
groupModels, err := mgr.ListRemoteGroup(ctx, "whisper")

// List versions of a specific model
versions, err := mgr.ListRemoteVersions(ctx, "whisper", "ggml-medium")
```

### Downloading Models

```go
ref := models.ModelRef{Group: "whisper", Model: "ggml-medium", Version: "q8_0"}

// Simple download
err := mgr.Pull(ctx, ref)

// Download with progress reporting
err := mgr.Pull(ctx, ref, models.WithProgress(func(p models.PullProgress) {
    switch p.Phase {
    case "manifest":
        fmt.Println("Fetching manifest...")
    case "chunks":
        pct := float64(p.BytesCompleted) / float64(p.BytesTotal) * 100
        fmt.Printf("\rDownloading: %.1f%%", pct)
    case "extracting":
        fmt.Printf("\rExtracting: %s", p.CurrentFile)
    }
}))

// Force re-download even if already installed
err := mgr.Pull(ctx, ref, models.WithForce())

// Custom concurrency (default is 4 parallel downloads)
err := mgr.Pull(ctx, ref, models.WithConcurrency(8))
```

### Checking Installation Status

```go
ref := models.ModelRef{Group: "whisper", Model: "ggml-medium", Version: "q8_0"}

// Check if a specific model is installed
info, err := mgr.GetInstalled(ctx, ref)
if errors.Is(err, models.ErrNotInstalled) {
    fmt.Println("Model not installed")
} else {
    fmt.Printf("Installed at: %s\n", info.Path)
    fmt.Printf("Size: %d bytes\n", info.TotalSize)
}

// Get path to installed model
path, err := mgr.Path(ctx, ref)
if err == nil {
    // Use the model at 'path'
}
```

### Checking for Updates

```go
ref := models.ModelRef{Group: "whisper", Model: "ggml-medium", Version: "q8_0"}

installedHash, remoteHash, err := mgr.CheckUpdate(ctx, ref)
if err != nil {
    log.Fatal(err)
}

if installedHash != remoteHash {
    fmt.Println("Update available!")
    // Download the update
    err = mgr.Pull(ctx, ref, models.WithForce())
}
```

### Removing Models

```go
ref := models.ModelRef{Group: "whisper", Model: "ggml-medium", Version: "q8_0"}

// Remove a specific version
err := mgr.Remove(ctx, ref)

// Remove all versions of a model
err := mgr.RemoveAll(ctx, "whisper", "ggml-medium")
```

### Cache Management

```go
// Clear the chunk cache (frees disk space from incomplete downloads)
err := mgr.PruneCache(ctx)
```

### Auto-Download Pattern

A common pattern is to automatically download a model if it's not installed:

```go
func EnsureModel(ctx context.Context, mgr models.Manager, group, model, version string) (string, error) {
    ref := models.ModelRef{Group: group, Model: model, Version: version}

    // Check if already installed
    path, err := mgr.Path(ctx, ref)
    if err == nil {
        return path, nil // Already installed
    }

    if !errors.Is(err, models.ErrNotInstalled) {
        return "", err // Unexpected error
    }

    // Download the model
    fmt.Printf("Downloading %s/%s %s...\n", group, model, version)
    if err := mgr.Pull(ctx, ref); err != nil {
        return "", fmt.Errorf("failed to download model: %w", err)
    }

    // Return the path
    return mgr.Path(ctx, ref)
}
```

## Progress Reporting

The `PullProgress` struct provides detailed download progress:

```go
type PullProgress struct {
    Phase           string // "manifest", "chunks", or "extracting"
    ChunksTotal     int    // Total chunks to download
    ChunksCompleted int    // Chunks downloaded so far
    BytesTotal      int64  // Total bytes to download
    BytesCompleted  int64  // Bytes from completed chunks
    BytesDownloaded int64  // Actual network bytes (excludes cache hits)
    BytesInProgress int64  // Bytes currently downloading
    CurrentFile     string // File being extracted (extracting phase only)
}
```

For accurate speed calculation, use `BytesDownloaded + BytesInProgress`:

```go
models.WithProgress(func(p models.PullProgress) {
    if p.Phase == "chunks" {
        elapsed := time.Since(startTime).Seconds()
        speed := float64(p.BytesDownloaded + p.BytesInProgress) / elapsed
        fmt.Printf("Speed: %.2f MB/s\n", speed / 1024 / 1024)
    }
})
```

## Error Handling

The module provides sentinel errors for common conditions:

```go
import "errors"

err := mgr.Pull(ctx, ref)
switch {
case errors.Is(err, models.ErrModelNotFound):
    // Model doesn't exist in registry
case errors.Is(err, models.ErrVersionNotFound):
    // Version doesn't exist for this model
case errors.Is(err, models.ErrNotInstalled):
    // Model not installed locally
case errors.Is(err, models.ErrAlreadyInstalled):
    // Model already installed (use WithForce to re-download)
case errors.Is(err, models.ErrHashMismatch):
    // Downloaded data failed verification
case errors.Is(err, models.ErrNetworkError):
    // Network or connection failure
case errors.Is(err, models.ErrStorageError):
    // Filesystem operation failed
case errors.Is(err, models.ErrInvalidRef):
    // Invalid model reference format
case errors.Is(err, models.ErrRegistryError):
    // Registry returned invalid data
}
```

## Storage Locations

Default storage paths by platform:

| Platform | Default Path |
|----------|--------------|
| macOS    | `~/Library/Application Support/<appName>/models/` |
| Linux    | `~/.local/share/<appName>/models/` (or `$XDG_DATA_HOME/<appName>/models/`) |
| Windows  | `%APPDATA%\<appName>\models\` |

Override with `DataDir` in config or `<APPNAME>_MODELS_DIR` environment variable.

## License

[Your License Here]
