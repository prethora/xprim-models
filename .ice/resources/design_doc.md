# xprim-models Design Document

## Overview

The `xprim-models` module is a reusable Go package for consuming machine learning models from an XPRIM content-addressed model registry. It provides a complete solution for downloading, verifying, caching, and managing ML models stored on remote registries.

The module serves two primary use cases:

1. **Programmatic API** - A `Manager` interface for applications that need to fetch and manage models as part of their runtime behavior
2. **Embeddable CLI** - A Cobra command tree that parent CLI tools can attach to provide `<tool> models ...` subcommands

All downloads are verified using SHA-256 content addressing, ensuring integrity and enabling efficient caching and deduplication.

## High-Level Component Organization

The module is organized into five major internal components:

### Public API Layer

The external interface exposed to consumers. This includes:

- **Manager interface** - Programmatic access to all model management operations (list, pull, remove, path lookup, update checking)
- **Cobra command factory** - `NewCommand()` function that returns a configured command tree ready to attach to any parent CLI
- **Configuration types** - `Config`, `ModelRef`, and option types that consumers use to configure the module
- **Result types** - `InstalledModel`, `RemoteModel`, `PullProgress`, and similar types returned by operations

The Public API layer is the only component with exported symbols. All other components are internal.

### Registry Client

Handles all communication with the remote model registry:

- Fetches `models.json` index to discover available models and their manifest hashes
- Retrieves manifests from `data/<manifest-hash>` paths
- Downloads chunks from `data/<chunk-hash>` paths
- Manages HTTP client lifecycle and configuration
- Handles network errors and retries

The Registry Client has no knowledge of local storage—it only knows how to fetch bytes from URLs and return them to callers.

### Storage Layer

Manages all local filesystem operations with platform awareness:

- Determines platform-appropriate base directories (XDG on Linux, Application Support on macOS, AppData on Windows)
- Maintains the local `registry.json` tracking installed models
- Creates and manages the directory hierarchy for model storage
- Provides atomic write operations to prevent corruption
- Handles the chunk cache directory for download resume

The Storage Layer abstracts all platform-specific path logic, presenting a uniform interface to other components.

### Download Engine

Orchestrates the complete download pipeline:

- Coordinates manifest fetching and parsing
- Manages concurrent chunk downloads with configurable parallelism
- Verifies chunk integrity against SHA-256 hashes
- Stores verified chunks in the cache for resume capability
- Tracks progress and invokes progress callbacks
- Handles partial failure and cleanup

The Download Engine uses the Registry Client to fetch data and the Storage Layer to persist it.

### File Reconstructor

Reassembles model files from downloaded chunks:

- Reads chunks in order from the cache
- Streams data through to create final model files
- Verifies total size matches manifest expectations
- Creates directory structure as needed for nested files
- Cleans up chunk cache after successful reconstruction

The File Reconstructor operates on cached chunks and writes final files to the Storage Layer.

## Component Interaction

```
┌─────────────────────────────────────────────────────────────┐
│                     Public API Layer                        │
│              (Manager interface, Cobra commands)            │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          │ orchestrates
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                     Download Engine                         │
│           (pipeline coordination, concurrency)              │
└──────────┬─────────────────────────────────┬────────────────┘
           │                                 │
           │ fetches from                    │ persists to
           ▼                                 ▼
┌─────────────────────┐           ┌─────────────────────────┐
│   Registry Client   │           │     Storage Layer       │
│  (HTTP, remote I/O) │           │ (filesystem, platform)  │
└─────────────────────┘           └───────────┬─────────────┘
                                              │
                                              │ writes to
                                              ▼
                                  ┌─────────────────────────┐
                                  │   File Reconstructor    │
                                  │  (chunk → file assembly)│
                                  └─────────────────────────┘
```

**Dependency Direction:**
- Public API Layer depends on Download Engine (and transitively on all components)
- Download Engine depends on Registry Client and Storage Layer
- File Reconstructor depends on Storage Layer
- Registry Client and Storage Layer are independent of each other

This layering ensures clear separation of concerns and enables testing each component in isolation.

## Design Principles

### Content-Addressed Integrity

Every piece of data in the system is verified by its SHA-256 hash:

- Model versions in `models.json` reference manifests by hash
- Manifests reference chunks by hash
- All downloads are verified before use

This provides end-to-end integrity: if a download completes without error, the data is guaranteed correct.

### Platform Abstraction

Platform-specific behavior is isolated in the Storage Layer:

- Path determination (XDG, Application Support, AppData) happens in one place
- Business logic components never import `runtime` for GOOS checks
- All path manipulation uses `path/filepath` for portability
- Environment variable overrides provide escape hatches when needed

### Testability

The design prioritizes testability:

- HTTP client is injected via interface, enabling mock servers in tests
- No global state—all configuration passed explicitly at construction
- Storage operations can be tested with temporary directories
- Components can be tested in isolation by mocking their dependencies

### Embeddability

The module is designed for seamless embedding:

- `NewCommand()` returns a detached Cobra command tree
- Parent CLIs attach it with a single `AddCommand()` call
- App name configuration drives storage paths, so each parent tool gets isolated storage
- No init() functions or side effects on import

## External Dependencies

The module maintains a minimal dependency footprint, relying primarily on the Go standard library with two external dependencies for CLI functionality.

### Cobra (github.com/spf13/cobra) - v1.8.x

**Purpose:** CLI framework enabling the embeddable command tree pattern.

**Why Cobra:**
- Industry standard for Go CLI applications (used by kubectl, Hugo, GitHub CLI)
- Excellent support for subcommand composition and attachment
- Mature, stable, actively maintained
- Rich feature set for flags, help generation, and shell completion

**Architectural Implications:**
- Commands are constructed programmatically via `NewCommand()`, not via `init()` functions
- This ensures no side effects on import—the parent CLI controls when commands are instantiated
- The parent CLI owns the root command; this module provides a detached subtree that attaches cleanly

### Progressbar (github.com/schollz/progressbar/v3) - v3.17.x

**Purpose:** Terminal progress display for download operations in CLI mode.

**Why progressbar/v3:**
- Thread-safe design suitable for concurrent chunk downloads
- Good terminal handling across platforms
- Customizable appearance and update frequency
- Actively maintained with recent releases

**Architectural Implications:**
- Used only in the CLI layer, not in the Manager interface
- The programmatic API uses progress callbacks (`WithProgress(func(PullProgress))`) instead
- This keeps the Manager interface UI-agnostic—consumers can implement their own progress display
- CLI commands translate callback events into progressbar updates

### Standard Library Usage

All core functionality uses Go standard library packages:

| Package | Purpose |
|---------|---------|
| `net/http` | HTTP client for registry communication |
| `crypto/sha256` | Content verification for manifests and chunks |
| `encoding/json` | Parsing registry index and manifests |
| `path/filepath` | Cross-platform path manipulation |
| `os` | File operations and environment variables |
| `context` | Request cancellation and timeouts |
| `sync` | Concurrency primitives for parallel downloads |
| `io` | Streaming interfaces for efficient data handling |

This approach minimizes the dependency footprint, reduces supply chain risk, and ensures long-term stability.

### Version Constraints

**Versioning Strategy:**
- Pin to minor versions (v1.8.x, v3.17.x) to receive patch updates while avoiding breaking changes
- Use Go modules with minimum version selection (MVS)
- No vendoring required; standard module workflow

**go.mod excerpt:**
```
require (
    github.com/spf13/cobra v1.8.0
    github.com/schollz/progressbar/v3 v3.17.0
)
```

### Alternatives Considered

**CLI Framework:**
- `urfave/cli` was considered but Cobra has better subcommand composition for the embedding use case
- Cobra's command tree model maps directly to the "attach a subtree" pattern needed here

**Progress Display:**
- `cheggaaa/pb` (v3.1.5) is a viable alternative with similar features
- `progressbar/v3` was chosen for its explicit thread-safety guarantees and cleaner API for concurrent updates

## Storage Architecture

The Storage Layer manages all local filesystem operations, abstracting platform differences and providing atomic operations to ensure data integrity.

### Platform-Specific Base Directories

The module stores data in platform-appropriate locations, following each operating system's conventions:

| Platform | Default Base Path |
|----------|-------------------|
| Linux | `$XDG_DATA_HOME/<app>/models/` or `~/.local/share/<app>/models/` |
| macOS | `~/Library/Application Support/<app>/models/` |
| Windows | `%APPDATA%\<app>\models\` |

**Path Resolution Order:**

1. **Environment override** - If `<APPNAME>_MODELS_DIR` is set (e.g., `MYTOOL_MODELS_DIR`), use that path directly
2. **Config override** - If `Config.DataDir` is non-empty, use that path
3. **Platform default** - Use the platform-appropriate directory as shown above

The `<app>` placeholder is replaced with `Config.AppName`. This means each parent CLI that embeds this module gets isolated storage—a tool named "whisper-tool" stores models separately from one named "llama-cli", even if both use the same registry.

**Why These Locations:**
- Linux follows the XDG Base Directory Specification, the standard for user data
- macOS uses Application Support, the standard location for application-managed data
- Windows uses APPDATA (Roaming), appropriate for user-specific application data

### Directory Layout

```
<base>/
├── registry.json                      # Local index of installed models
├── cache/
│   └── chunks/                        # Temporary chunk cache for downloads
│       ├── a1b2c3d4e5f6...           # Chunk files named by SHA-256 hash
│       └── b2c3d4e5f6a1...
└── <group>/                           # Model group directory
    └── <model>/                       # Model name directory
        └── <version>/                 # Version directory
            ├── .<app>/                # Hidden metadata directory
            │   └── manifest.json      # Copy of manifest (for verification)
            ├── model.safetensors      # Extracted model files
            ├── config.json            # (varies by model)
            └── tokenizer.json         # (varies by model)
```

**Directory Hierarchy Rationale:**
- Three-level hierarchy (`group/model/version`) mirrors the `ModelRef` structure
- Allows multiple versions of the same model to coexist
- Easy manual inspection and cleanup by users if needed
- Hidden `.<app>/manifest.json` enables integrity verification and update detection while avoiding filename conflicts

### Local Registry (registry.json)

The local registry is a JSON file that tracks all installed models, enabling fast queries without filesystem scanning.

**Structure:**

```
{
  "<group>": {
    "<model>": {
      "<version>": {
        "manifest_hash": "<sha256>",
        "total_size": <bytes>,
        "file_count": <int>,
        "installed_at": "<ISO8601 timestamp>"
      }
    }
  }
}
```

**What It Tracks:**
- **manifest_hash** - SHA-256 of the manifest, used for update detection
- **total_size** - Total bytes of all model files, for display and disk usage reporting
- **file_count** - Number of files in the model
- **installed_at** - Timestamp of installation, for display and potential cleanup policies

**Why a Separate Registry:**
- Avoids scanning potentially thousands of files to list installed models
- Enables O(1) lookup for "is this model installed?"
- Stores metadata that isn't readily available from the filesystem alone
- Single source of truth for what's installed (directory presence without registry entry = incomplete installation)

### Chunk Cache Strategy

The chunk cache enables resumable downloads and efficient handling of interrupted transfers.

**Location:** `<base>/cache/chunks/`

**Naming:** Each chunk file is named by its SHA-256 hash (the same hash used for verification).

**Lifecycle:**

1. **During download** - As each chunk is downloaded and verified, it's stored in the cache
2. **On success** - After model files are reconstructed, chunks for that model are deleted
3. **On failure** - Chunks remain in the cache, enabling resume on the next attempt
4. **Cleanup** - Chunks older than 24 hours are periodically removed (stale incomplete downloads)

**Verification on Read:**
- When reading a cached chunk, its hash is verified against its filename
- Corrupted chunks are deleted and re-downloaded
- This catches filesystem corruption or incomplete writes

**Why This Approach:**
- Content-addressed naming means duplicate chunks (across models) are automatically deduplicated
- Hash verification ensures integrity even if the cache is corrupted
- Keeping chunks on failure avoids re-downloading potentially gigabytes of data
- Time-based cleanup prevents unbounded cache growth from abandoned downloads

### Atomic Operations

The Storage Layer uses atomic operations to prevent corruption from crashes or interruptions.

**Write Pattern:**
1. Write data to a temporary file in the same directory
2. Sync the file to ensure data reaches disk
3. Rename the temporary file to the final name (atomic on POSIX and Windows)

**Registry Updates:**
- The registry.json file is always updated atomically using the write-then-rename pattern
- Concurrent reads are safe; writes are serialized internally via mutex
- Cross-process writes are serialized using file locking (`flock` on Unix, `LockFileEx` on Windows)
- Lock file pattern: `registry.json.lock` in the same directory
- Lock acquisition uses polling with exponential backoff (10ms → 100ms) with a 30-second timeout

**Model Installation Atomicity:**
- Individual files are written atomically
- The model is only added to registry.json after ALL files are successfully written
- If installation fails partway through, the partially-written files are cleaned up
- This ensures the registry only contains complete, usable models

**Why Atomicity Matters:**
- Power loss or crash during write won't corrupt existing data
- Concurrent operations (multiple goroutines listing while another installs) are safe
- Users can trust that if a model appears in `list`, it's fully usable

## Download Pipeline

The Download Engine orchestrates the complete flow from a `Pull()` call to a fully installed, verified model.

### Pipeline Overview

```
Pull(ModelRef)
     │
     ▼
┌─────────────────────────────────────┐
│  1. Fetch models.json from registry │
│     Get manifest hash for model     │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  2. Resolve aliases                 │
│     If version is alias → follow    │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  3. Check local registry            │
│     Same hash? → Already installed  │
│     (return early unless --force)   │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  4. Fetch manifest                  │
│     GET data/<manifest-hash>        │
│     Verify hash, parse JSON         │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  5. Download chunks (concurrent)    │
│     N workers fetch in parallel     │
│     Verify each chunk's hash        │
│     Store verified chunks in cache  │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  6. Reconstruct files               │
│     Stream chunks → extract files   │
│     Verify total size               │
└─────────────────┬───────────────────┘
                  │
                  ▼
┌─────────────────────────────────────┐
│  7. Finalize installation           │
│     Update registry.json            │
│     Clean up chunk cache            │
└─────────────────────────────────────┘
```

### Concurrent Chunk Downloads

The Download Engine uses a worker pool pattern for parallel chunk downloads.

**Concurrency Configuration:**
- Default: 4 simultaneous chunk downloads
- Maximum: 16 (enforced regardless of configuration)
- Configurable via `WithConcurrency(n)` option

**Worker Pool Architecture:**

```
                    ┌─────────────┐
                    │ Chunk Queue │ (channel of chunk metadata)
                    └──────┬──────┘
                           │
           ┌───────────────┼───────────────┐
           │               │               │
           ▼               ▼               ▼
      ┌─────────┐     ┌─────────┐     ┌─────────┐
      │Worker 1 │     │Worker 2 │     │Worker N │
      └────┬────┘     └────┬────┘     └────┬────┘
           │               │               │
           │  fetch → verify → cache → progress
           │               │               │
           ▼               ▼               ▼
                    ┌─────────────┐
                    │   Results   │ (errors collected)
                    └─────────────┘
```

**Worker Behavior:**
1. Pull chunk metadata from queue (hash, index, size)
2. Check if chunk already cached (resume capability)
3. If not cached: fetch from `data/<chunk-hash>`
4. Verify downloaded data against expected hash
5. Store verified chunk in cache
6. Report progress via callback
7. Repeat until queue empty or error

**Coordination:**
- WaitGroup tracks active workers
- Error channel collects failures
- Context cancellation triggers clean shutdown
- Early termination: if any chunk fails after retries, remaining work is cancelled

### Progress Reporting

Progress information flows from the Download Engine to consumers via callbacks.

**Progress Phases:**
1. **"manifest"** - Fetching and parsing the manifest
2. **"chunks"** - Downloading and verifying chunks
3. **"extracting"** - Reconstructing files from chunks

**Progress Data:**
- `ChunksTotal` / `ChunksCompleted` - Chunk download progress
- `BytesTotal` / `BytesCompleted` - Byte-level progress for accurate percentage
- `CurrentFile` - During extraction, which file is being written

**Thread Safety:**
- Progress callbacks are invoked from download worker goroutines
- Callbacks MUST be thread-safe (multiple workers report concurrently)
- The callback should be lightweight—heavy processing blocks the worker

**CLI Progress Display:**

The CLI provides an enhanced progress bar with:
- **Download speed** - Real-time network speed in KB/s or MB/s with byte-level accuracy
- **Elapsed time** - Human-readable format (e.g., "5s", "2m 30s", "1h 5m")
- **Estimated time remaining** - Human-readable format based on current network speed
- **Byte progress** - Shows bytes completed vs total (includes both cached and downloaded)

Example output:
```
Downloading [============>                 ] 45% (5.2 MB/s, elapsed: 30s, remaining: 2m 15s)
```

**Smooth Speed Calculation:**

The speed calculation uses byte-level progress for accuracy:
- `BytesDownloaded` - Cumulative bytes from completed chunk downloads (excludes cache hits)
- `BytesInProgress` - Bytes currently being read across all download workers
- Speed = `(BytesDownloaded + BytesInProgress) / elapsed`

This approach provides smooth, accurate speed readings because:
- Bytes are counted as they're read from the network, not just when chunks complete
- Multiple concurrent downloads all contribute to the in-progress count
- Cache hits (instant completions) don't inflate the speed
- Speed updates smoothly even during large chunk downloads

The progress bar updates:
- Every second (via a background ticker) for smooth time/speed updates
- Immediately when a chunk completes for accurate byte progress

This dual-update mechanism ensures the display remains responsive even when chunks are large.

**CLI vs Programmatic:**
- CLI commands register a callback that updates the progressbar
- Programmatic API consumers provide their own callback or omit it
- Without a callback, downloads proceed silently (useful for automated scripts)

### Retry and Failure Handling

The Download Engine includes resilience mechanisms for unreliable networks.

**Network Errors:**
- Retry up to 3 times with exponential backoff (1s, 2s, 4s)
- Includes connection errors, timeouts, and 5xx server errors
- 4xx errors (except 429) are not retried (client error, won't succeed)
- 429 (rate limited) is retried with longer backoff

**Hash Mismatch:**
- If downloaded data doesn't match expected hash, delete the corrupted data
- Retry the download (counts against retry limit)
- Persistent mismatch indicates registry corruption or attack—fail with `ErrHashMismatch`

**Partial Failure:**
- If download fails after retries, cached chunks are retained
- Next pull attempt will skip already-cached chunks (resume)
- Incomplete model is NOT added to registry.json
- User sees error but can retry without re-downloading everything

**Context Cancellation:**
- `ctx.Done()` triggers clean shutdown
- Active downloads are abandoned (HTTP requests cancelled)
- Cached chunks are preserved for potential resume
- Returns `ctx.Err()` to caller

### File Reconstruction

After all chunks are downloaded, the File Reconstructor assembles the final model files.

**Conceptual Model:**
- Chunks are logically concatenated in index order to form a single byte stream
- Files are defined by the manifest with `path` and `size` fields
- Files are extracted sequentially: read `size` bytes, write to `path`

**Streaming Approach:**
- Chunks are NOT loaded entirely into memory
- A reader abstraction presents cached chunks as a continuous stream
- Files are written as bytes are read—memory usage stays bounded
- This enables handling models larger than available RAM

**Directory Creation:**
- File paths may include subdirectories (e.g., `subdir/model.bin`)
- Parent directories are created as needed before writing each file
- Permissions follow platform defaults (user read/write)

**Verification:**
- Total bytes extracted must equal `manifest.total_size`
- Mismatch indicates manifest corruption or chunk ordering error
- Fails with error; partially-written files are cleaned up

**Completion:**
- After all files written, store manifest in `.<app>/manifest.json`
- Add entry to registry.json (atomic update)
- Delete cached chunks for this model (successful install)
- Return success to caller

## Error Handling

The module uses a structured approach to error handling that enables both programmatic error checking and human-readable error messages.

### Error Categories

Errors fall into distinct categories based on their cause and appropriate response:

**Registry Errors:**
- Model does not exist in the remote registry
- Requested version does not exist for the model
- Registry returned malformed or unparseable data
- Manifest data is invalid or corrupted

**Network Errors:**
- Connection refused or unreachable host
- Request timeout (configurable, default 30 seconds for chunks)
- Server errors (5xx responses)
- TLS/certificate errors

**Verification Errors:**
- Downloaded chunk hash doesn't match expected hash
- Manifest hash doesn't match expected hash
- Total extracted size doesn't match manifest

**Storage Errors:**
- Disk full (no space left on device)
- Permission denied (cannot write to storage directory)
- Path too long (Windows limitation)
- I/O errors during read or write

**Usage Errors:**
- Invalid model reference format (must be `group/model version`)
- Invalid configuration (empty AppName, malformed RegistryURL)
- Invalid option values (concurrency out of range)

### Sentinel Errors

The module exports sentinel errors that consumers can check programmatically using `errors.Is()`:

| Sentinel | Meaning |
|----------|---------|
| `ErrModelNotFound` | Model does not exist in the remote registry |
| `ErrVersionNotFound` | Version does not exist for the specified model |
| `ErrNotInstalled` | Model is not installed locally |
| `ErrAlreadyInstalled` | Model already installed (returned without `--force`) |
| `ErrHashMismatch` | Downloaded data failed hash verification |
| `ErrNetworkError` | Network or connection failure |
| `ErrStorageError` | Filesystem operation failed |
| `ErrInvalidRef` | Model reference format is invalid |
| `ErrRegistryError` | Registry returned invalid or unparseable data |

**Usage Example (conceptual):**
```
err := mgr.Pull(ctx, ref)
if errors.Is(err, models.ErrNotInstalled) {
    // Handle "not installed" case
}
```

### Error Wrapping

All errors include contextual information while preserving the ability to check error types:

**Wrapping Strategy:**
- Errors wrap the underlying cause using `fmt.Errorf("context: %w", err)`
- Context includes: operation name, model reference, file paths, URLs as appropriate
- Original errors are preserved—`errors.Unwrap()` and `errors.Is()` work correctly

**Example Error Chain:**
```
models: pull fast-whisper/tiny fp16: download chunk 3/8: network error: connection timeout
        ↑                            ↑                    ↑              ↑
        operation                    specific step        category       root cause
```

**Why This Approach:**
- Programmatic handling: `errors.Is(err, ErrNetworkError)` returns true
- Human readability: full error message explains what happened
- Debugging: complete chain shows where the error originated

### CLI Exit Codes

The CLI layer maps errors to specific exit codes for scripting integration:

| Exit Code | Meaning | Corresponding Error |
|-----------|---------|---------------------|
| 0 | Success | (no error) |
| 1 | General/unknown error | Unexpected errors |
| 2 | Invalid arguments | Bad flags, missing required args |
| 3 | Model not found | `ErrModelNotFound`, `ErrVersionNotFound` |
| 4 | Model not installed | `ErrNotInstalled` |
| 5 | Network error | `ErrNetworkError` |
| 6 | Verification failed | `ErrHashMismatch` |
| 7 | Storage error | `ErrStorageError` |

**Usage in Scripts:**
```
mytool models pull fast-whisper/tiny fp16
case $? in
    0) echo "Success" ;;
    3) echo "Model not found in registry" ;;
    5) echo "Network error - check connection" ;;
    *) echo "Failed with code $?" ;;
esac
```

## Concurrency and Thread Safety

The module is designed for safe concurrent use in multi-goroutine applications.

### Manager Thread Safety

**The Manager interface is fully thread-safe.** Multiple goroutines can call Manager methods simultaneously without external synchronization.

**Guarantees:**
- All Manager methods can be called concurrently
- Internal state (registry.json, cache) is protected by appropriate synchronization
- No data races under concurrent access

**Concurrent Operations:**

| Scenario | Behavior |
|----------|----------|
| Multiple List calls | Execute concurrently, read-only |
| List during Pull | Safe—Pull updates registry atomically |
| Pull different models | Execute concurrently, independent |
| Pull same model twice | Serialized—second call waits or returns early |
| Remove during Pull | Safe—Remove waits for Pull to complete |

**Same-Model Serialization:**
- Concurrent pulls of the SAME model (same group/model/version) are serialized
- First caller proceeds; others wait for completion or return `ErrAlreadyInstalled`
- Prevents redundant downloads and cache conflicts

### Progress Callback Thread Safety

**Progress callbacks are invoked from multiple goroutines.** Callback implementations MUST be thread-safe.

**Why:**
- Multiple download workers run concurrently
- Each worker reports progress independently
- Callbacks may be invoked simultaneously from different workers

**Implementation Requirements:**
- Use synchronization if the callback updates shared state
- Avoid blocking operations in callbacks (delays download workers)
- The CLI's progressbar wrapper handles this internally

**Safe Callback Example (conceptual):**
```
var mu sync.Mutex
var totalBytes int64

callback := func(p PullProgress) {
    mu.Lock()
    totalBytes = p.BytesCompleted
    mu.Unlock()
}
```

### Storage Synchronization

Internal synchronization ensures storage operations are safe under concurrent access.

**Registry.json:**
- Reads are lock-free (file is replaced atomically, readers see consistent state)
- Writes use dual-layer protection:
  - In-process: `sync.RWMutex` serializes writes from concurrent goroutines
  - Cross-process: File locking (`flock`/`LockFileEx`) serializes writes from multiple processes
- Lock file: `registry.json.lock` with 30-second timeout and exponential backoff
- Atomic replacement (write-then-rename) ensures readers never see partial writes

**Chunk Cache:**
- Content-addressed naming eliminates conflicts (each chunk has unique hash-based name)
- Same chunk written by concurrent downloads is idempotent (same content)
- Cleanup operations are coordinated with active downloads

**Model Directories:**
- Each model version has its own directory—no cross-model conflicts
- Same-model serialization (above) prevents conflicts within a version
- Directory creation is idempotent (create if not exists)

### Context and Cancellation

All operations respect context cancellation:

- Pass `context.Context` to all Manager methods
- Cancellation propagates to HTTP requests and file operations
- Clean shutdown: no goroutine leaks, no corrupted state
- Cached chunks preserved for potential resume

## Testing Strategy

The module is designed for comprehensive testing without requiring network access or external services.

### Testing Philosophy

**Guiding Principles:**
- All tests run without network access (CI-friendly)
- Each component can be tested in isolation
- Dependencies are injected, not hardcoded
- Tests verify behavior, not implementation details

**Test Categories:**

| Category | Scope | Network | Filesystem |
|----------|-------|---------|------------|
| Unit | Single component | Mock | Mock or temp |
| Integration | Multiple components | Mock | Real (temp dirs) |
| End-to-end | Full workflows | Mock | Real (temp dirs) |

### Unit Testing by Component

**Registry Client:**
- Mock HTTP client returns canned JSON responses
- Test parsing of models.json, manifests
- Test error handling: malformed JSON, missing fields, HTTP errors
- Test URL construction for different endpoints

**Storage Layer:**
- Use temporary directories created per-test
- Test platform path logic with explicit GOOS values
- Test atomic write operations (verify no partial writes visible)
- Test registry.json read/write/update operations
- Test chunk cache operations: write, read, verify, cleanup

**Download Engine:**
- Mock both Registry Client and Storage Layer
- Test concurrency: verify N workers created, proper coordination
- Test progress reporting: callback invoked with correct values
- Test failure scenarios: retry logic, early termination
- Test resume: cached chunks skipped, only missing chunks fetched

**File Reconstructor:**
- Provide mock chunk data as byte arrays
- Test streaming: memory usage bounded regardless of model size
- Test size verification: correct total, incorrect total
- Test directory creation for nested file paths

### Mock HTTP Client

The HTTPClient interface enables complete test isolation from the network.

**Mock Implementation Approach:**
- Accept a map of URL → response (status code, body, headers)
- Support error injection (connection refused, timeout)
- Support latency injection for concurrency testing
- Track request count for verification

**Test Scenarios Enabled:**
- Normal operation: all requests succeed
- Network failure: connection errors, timeouts
- Server errors: 500, 502, 503 responses
- Rate limiting: 429 with retry-after
- Hash mismatch: return corrupted chunk data
- Partial failure: some chunks succeed, others fail

**Example Test Pattern (conceptual):**
```
mockClient := NewMockHTTPClient()
mockClient.On("GET", "https://registry/models.json").Return(200, modelsJSON)
mockClient.On("GET", "https://registry/data/abc123").Return(200, manifestData)

mgr := NewManager(cfg, WithHTTPClient(mockClient))
err := mgr.Pull(ctx, ref)
// Assert success, verify mock was called correctly
```

### Integration Testing

Integration tests verify that components work together correctly.

**Full Pull Workflow:**
- Create mock HTTP client with complete registry data
- Use real Storage Layer with temporary directory
- Execute Pull() and verify:
  - Files extracted correctly
  - registry.json updated
  - Chunk cache cleaned up

**Concurrent Operations:**
- Spawn multiple goroutines calling Manager methods
- Verify no data races (run with `-race` flag)
- Verify correct behavior under concurrent pull of same model

**Resume Capability:**
- Start a download, cancel context mid-way
- Verify chunks are cached
- Restart download, verify cached chunks reused
- Verify final model is correct

**Remove Operations:**
- Install model, then remove
- Verify files deleted
- Verify registry.json updated
- Verify RemoveAll removes all versions

### Cross-Platform Testing

The module must work correctly on all target platforms.

**CI Matrix:**

| Platform | Architecture | Notes |
|----------|--------------|-------|
| Linux | amd64 | Primary CI platform |
| Linux | arm64 | If CI supports |
| macOS | arm64 | Apple Silicon |
| macOS | amd64 | Intel Mac |
| Windows | amd64 | Windows Server |

**Platform-Specific Test Concerns:**
- Path separator handling (verify `filepath` used consistently)
- Storage directory defaults (verify correct for each OS)
- Atomic rename behavior (should work on all platforms)
- Long path handling on Windows

**Test Assertions:**
- Use `filepath.Join()` for all path comparisons in tests
- Don't hardcode `/` or `\` in expected paths
- Verify behavior, not specific path strings

### Test CLI as API Validation

The `xprim-models` test CLI serves a dual purpose: testing tool and API validation.

**API Surface Validation:**
- Test CLI imports ONLY the public `models` package
- Test CLI uses ONLY exported types and functions
- If the CLI can't perform an operation, the public API is incomplete

**What This Catches:**
- Missing exported types that consumers need
- Inadequate information in returned types
- Operations that require internal access

**Build Constraint:**
- Test CLI is in a separate module or uses build tags
- Cannot import internal packages (enforced by Go module boundaries)
- Serves as a real-world consumer of the API

### Testing Boundaries

**What's Tested in CI:**
- All unit tests (no network, fast)
- All integration tests (mock network, temp filesystem)
- Race detection (`go test -race`)
- All platforms in CI matrix

**What's NOT Tested in CI:**
- Real registry access (requires network, external dependency)
- Performance benchmarks (optional, run manually)
- Visual progress bar rendering (requires terminal)
- Large model downloads (resource-intensive)

**Manual Testing:**
- Test CLI against real registry for smoke testing
- Visual verification of progress bar behavior
- Performance profiling for optimization work

## Build and Distribution

The module is designed for easy building, cross-compilation, and distribution as a Go package.

### Target Platforms

| Platform | Architecture | Status |
|----------|--------------|--------|
| macOS | arm64 (Apple Silicon) | Primary |
| macOS | amd64 (Intel) | Primary |
| Linux | amd64 | Primary |
| Linux | arm64 | Primary |
| Windows | amd64 | Primary |

**No CGO Required:**
- The module is pure Go with no C dependencies
- All external dependencies (Cobra, progressbar) are also pure Go
- This enables easy cross-compilation from any platform

### Build Process

**Standard Go Build:**
- Use `go build` with standard Go toolchain
- No external build tools required (no Make, no shell scripts needed)
- Module dependencies managed via go.mod

**Cross-Compilation:**
```
# Build for different platforms from any host
GOOS=linux GOARCH=amd64 go build ./...
GOOS=darwin GOARCH=arm64 go build ./...
GOOS=windows GOARCH=amd64 go build ./...
```

**Reproducible Builds:**
- Same source code produces identical binaries
- go.sum ensures exact dependency versions
- No build-time code generation or timestamps

### CI/CD Considerations

**CI Requirements:**
- Go toolchain (version specified in go.mod)
- No external services or network access
- No database or cache services
- Self-contained test execution

**CI Pipeline Steps:**
1. Checkout code
2. Download dependencies (`go mod download`)
3. Run tests with race detector (`go test -race ./...`)
4. Build for all target platforms
5. Upload coverage reports (optional)

**Multi-Platform CI Matrix:**
- Run tests on Linux, macOS, and Windows runners
- Verify platform-specific code paths work correctly
- Cross-compile binaries from Linux for efficiency

**CI Artifacts:**
- Test results and coverage reports
- Built binaries (for test CLI, if needed)
- No production binaries (module is a library)

### Distribution Model

**As a Go Module:**
- Distributed via standard Go module system
- Consumers add to their go.mod: `require github.com/prethora/xprim-models v1.x.x`
- No binary distribution for the library itself
- Source code is the distribution format

**Versioning:**
- Semantic versioning (vMAJOR.MINOR.PATCH)
- Git tags mark releases (v1.0.0, v1.1.0, etc.)
- Major version changes for breaking API changes
- go.mod declares minimum Go version

**Test CLI:**
- Built by consumers or developers for testing
- Not distributed as a binary
- Serves as reference implementation and API validation

## CLI Output Modes

The CLI commands support multiple output modes for different use cases.

### Standard Output (Default)

Human-readable output optimized for terminal use:

**Progress Display:**
- Animated progress bar for downloads with byte-based tracking
- Download speed shown in KB/s or MB/s (auto-selected for shortest display)
- Elapsed time in human-readable format (e.g., "5s", "2m 30s", "1h 5m")
- Estimated time remaining in human-readable format
- Progress bar updates every second AND on each chunk completion
- Clear phase indicators (fetching manifest, downloading, extracting)

Example: `Downloading [============>                 ] 45% (5.2 MB/s, elapsed: 30s, remaining: 2m 15s)`

**List Output:**
- Formatted tables with aligned columns
- Human-readable sizes (MB, GB)
- Dates formatted for readability

**Status Messages:**
- Success confirmations with details
- Error messages with context
- Actionable suggestions on failure

### JSON Output (`--json`)

Machine-readable output for scripting and automation:

**Behavior:**
- All commands support the `--json` flag
- Output is valid JSON (parseable by `jq`, programming languages)
- One JSON object or array per command (not streaming)
- No progress bars or status messages mixed in

**Success Output:**
- Command result as JSON object or array
- Consistent field names across commands
- All relevant data included

**Error Output:**
- Errors also formatted as JSON when `--json` is set
- Includes error type, message, and context
- Exit code still indicates failure

**Use Cases:**
- Scripting model management operations
- Integration with other tools
- Automated workflows in CI/CD

### Quiet Mode (`--quiet`)

Minimal output for scripts that only care about exit codes:

**Behavior:**
- Suppress progress bars entirely
- Suppress informational messages
- Only output essential data (e.g., path for `models path` command)
- Errors still output to stderr

**Use Cases:**
- Scripts that check exit codes
- Background operations
- Reducing log noise in automated environments

### Verbose Mode (`--verbose`)

Detailed diagnostic output for troubleshooting:

**Additional Output:**
- Debug-level log messages
- HTTP request/response information (URLs, status codes, timing)
- File operation details
- Internal state information

**Implementation:**
- Verbose mode enables the Logger interface with debug level
- Logs to stderr (separate from command output)
- Does not affect JSON output format (logs go to stderr)

**Use Cases:**
- Debugging download failures
- Investigating performance issues
- Understanding module behavior

### Mode Combinations

| Flags | Behavior |
|-------|----------|
| (none) | Standard human-readable output |
| `--json` | JSON output, no progress |
| `--quiet` | Minimal output, no progress |
| `--verbose` | Standard output + debug logs |
| `--json --quiet` | JSON output only (quiet is redundant) |
| `--json --verbose` | JSON output + debug logs to stderr |

## Logging and Observability

The module supports optional structured logging for diagnostics and debugging.

### Logger Interface

The module accepts an optional Logger at construction time:

**Interface Design:**
- Four log levels: Debug, Info, Warn, Error
- Structured logging with key-value pairs
- Minimal interface—easy to adapt any logging library

**Behavior:**
- If no logger provided, logging is disabled (silent operation)
- Log calls are no-ops when logger is nil
- No performance penalty when logging disabled

**Integration:**
- Pass logger via `WithLogger(logger)` option to `NewManager()` or `NewCommand()`
- CLI commands use the logger for `--verbose` output
- Programmatic consumers can use their existing logging infrastructure

### What Gets Logged

**Debug Level:**
- HTTP requests: method, URL, timing
- HTTP responses: status code, content length, timing
- Cache operations: hit, miss, write, delete
- Chunk download progress details
- Internal state transitions

**Info Level:**
- Operation start: "pulling model fast-whisper/tiny fp16"
- Operation completion: "model installed successfully"
- Model removal: "removed fast-whisper/tiny fp16"
- Significant state changes

**Warn Level:**
- Retry attempts: "retrying chunk download (attempt 2/3)"
- Recoverable issues: "cached chunk corrupted, re-downloading"
- Non-fatal anomalies: "registry returned unexpected field"

**Error Level:**
- Failures that will be returned to caller
- Always includes context for debugging
- Logged before error is returned

### Logger Adapter Pattern

The Logger interface is intentionally minimal to support any logging library:

**Standard Library (log):**
- Wrap `log.Logger` to implement the interface
- Map levels to prefixes or ignore levels

**Popular Libraries:**
- slog: Direct mapping to slog levels
- zap: Wrap zap.Logger
- logrus: Wrap logrus.Logger
- zerolog: Wrap zerolog.Logger

**Example Adaptation (conceptual):**
```
type SlogAdapter struct {
    logger *slog.Logger
}

func (a *SlogAdapter) Debug(msg string, keysAndValues ...any) {
    a.logger.Debug(msg, keysAndValues...)
}
// ... similar for Info, Warn, Error
```

## API Operations Detail

Additional detail on specific Manager operations that require clarification.

### Update Checking (CheckUpdate)

The `CheckUpdate` method enables detection of updated model versions without automatic downloading.

**Operation Flow:**
1. Fetch models.json from registry
2. Look up the specified model/version
3. If version is an alias, resolve to current target
4. Compare remote manifest hash against installed manifest hash
5. Return both hashes to caller

**Return Values:**
- `installedHash`: SHA-256 of the installed model's manifest (empty if not installed)
- `remoteHash`: SHA-256 of the current manifest in registry
- If hashes differ, an update is available

**Design Decision - No Auto-Download:**
- `CheckUpdate` does NOT download automatically
- Caller receives information and decides whether to update
- Enables "check only" workflows and user confirmation
- CLI's `update --apply` performs download if hashes differ

**Use Cases:**
- Check if installed models are current
- Batch update checking across multiple models
- Notification systems for available updates

### GetRemoteDetail

The `GetRemoteDetail` method fetches comprehensive information about a model from the registry.

**What It Fetches:**
- Downloads the full manifest from `data/<manifest-hash>`
- Parses manifest to extract detailed information
- Does NOT download the model itself

**Information Returned:**
- Everything from `GetRemote` (ref, manifest hash, alias info)
- Plus: total size, chunk size, chunk count
- Plus: list of files with paths and sizes

**Use Cases:**
- Show model contents before downloading
- Display storage requirements
- Verify model contents match expectations
- CLI's `info --remote` command

**Performance Note:**
- Requires one additional HTTP request (manifest fetch)
- More expensive than `GetRemote` (which uses cached models.json)
- Cache manifest locally if needed for repeated queries

### RemoveAll

The `RemoveAll` method removes all installed versions of a model.

**Operation Flow:**
1. Query local registry for all versions under group/model
2. For each version found:
   - Delete model directory and all contents
   - Remove entry from registry.json
3. Return after all versions processed

**Behavior:**
- Removes all versions (fp16, int8, q4, etc.) of a single model
- Does NOT remove other models in the same group
- Updates registry.json after each successful removal

**Error Handling:**
- If any removal fails, operation continues with remaining versions
- Returns error describing what failed
- Partial removal is possible (some versions removed, others remain)
- Caller can retry to remove remaining versions

**CLI Usage:**
- `models remove fast-whisper/tiny --all`
- Confirms number of versions removed

### Prune Cache (PruneCache)

The `PruneCache` method removes all cached chunks from incomplete downloads.

**Operation Flow:**
1. Locate the chunk cache directory (`.chunks/`)
2. Remove the entire directory and all contents
3. Return success

**Behavior:**
- Removes all cached chunks regardless of age
- Frees disk space from incomplete or abandoned downloads
- Safe to call even if no cache exists
- Does NOT affect installed models

**Use Cases:**
- Free disk space after abandoned downloads
- Clean up after interrupted pull operations
- Periodic maintenance

**CLI Usage:**
- `models prune` - Prompts for confirmation before clearing
- `models prune -y` - Skips confirmation and clears immediately

### Confirmation Prompts

Destructive operations require user confirmation by default for safety.

**Commands with Confirmation:**
- `models remove` - Before removing a model or all versions
- `models prune` - Before clearing the chunk cache

**Confirmation Behavior:**
- Prompts display the action to be taken
- Default answer is "no" (pressing Enter aborts)
- User must type "y" or "yes" to confirm
- Operations abort gracefully on "n", "no", or empty input

**Skipping Confirmation:**
- Pass `-y` or `--yes` flag to skip the prompt
- Useful for scripts and automated workflows
- Example: `models remove fast-whisper/tiny fp16 -y`

**Design Rationale:**
- Default "no" prevents accidental data loss
- Explicit confirmation for destructive operations
- Override available for automation without sacrificing safety in interactive use

### Cobra Command Integration

The module provides a complete command tree for embedding in parent CLIs.

**Command Structure:**
```
models (root command)
├── list      List installed or remote models
├── pull      Download a model from registry
├── remove    Remove an installed model (with confirmation)
├── info      Show detailed model information
├── path      Output path to installed model
├── update    Check for or apply updates
└── prune     Clear the chunk cache (with confirmation)
```

**Integration Pattern:**
```
// Parent CLI creates the models command
modelsCmd := models.NewCommand(models.Config{
    AppName:     "myapp",
    RegistryURL: "https://registry.example.com",
})

// Parent CLI attaches to its root
rootCmd.AddCommand(modelsCmd)
```

**Flag Scoping:**
- Command-specific flags (e.g., `--force` on pull) only on that command
- Persistent flags (e.g., `--json`, `--quiet`, `--verbose`) on root `models` command
- Persistent flags automatically apply to all subcommands
- Parent CLI's flags do not conflict with models flags

**No Side Effects:**
- `NewCommand()` does not perform any I/O
- Manager is created lazily on first command execution
- Safe to call at CLI initialization time
- No `init()` functions that run on import
