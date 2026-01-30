// Package models provides functionality for consuming ML models from an
// XPRIM content-addressed model registry.
//
// The package serves two primary use cases:
//
//  1. Programmatic API via the Manager interface - Applications can use
//     NewManager to create a Manager that provides methods for listing,
//     downloading, and managing models.
//
//  2. Embeddable CLI via NewCommand - Parent CLI tools can attach a complete
//     "models" subcommand tree to their Cobra root command, providing commands
//     like "mytool models pull", "mytool models list", etc.
//
// # Thread Safety
//
// The Manager interface is fully thread-safe. All methods can be called
// concurrently from multiple goroutines without external synchronization.
//
// # Content Verification
//
// All downloads are verified using SHA-256 content addressing. Manifests and
// chunks are validated against their expected hashes, ensuring data integrity
// from registry to local storage.
//
// # Storage
//
// Models are stored in platform-appropriate directories:
//   - Linux: $XDG_DATA_HOME/<app>/models/ or ~/.local/share/<app>/models/
//   - macOS: ~/Library/Application Support/<app>/models/
//   - Windows: %APPDATA%\<app>\models\
//
// The storage location can be overridden via Config.DataDir or the
// <APPNAME>_MODELS_DIR environment variable.
package models
