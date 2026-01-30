package models

import (
	"strings"
	"time"
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
	// Group is the model group, e.g., "fast-whisper".
	Group string

	// Model is the model name within the group, e.g., "tiny".
	Model string

	// Version is the model version, e.g., "fp16", "int8", "q4".
	Version string
}

// String returns the canonical string form: "group/model version".
// If Version is empty, returns "group/model".
func (r ModelRef) String() string {
	if r.Version == "" {
		return r.Group + "/" + r.Model
	}
	return r.Group + "/" + r.Model + " " + r.Version
}

// ParseModelRef parses "group/model" or "group/model version" into a ModelRef.
// Returns ErrInvalidRef if the format is invalid.
func ParseModelRef(s string) (ModelRef, error) {
	if s == "" {
		return ModelRef{}, ErrInvalidRef
	}

	// Split on first space to separate "group/model" from optional "version"
	var groupModel, version string
	if idx := strings.Index(s, " "); idx != -1 {
		groupModel = s[:idx]
		version = s[idx+1:]
	} else {
		groupModel = s
	}

	// Split group/model on "/"
	parts := strings.Split(groupModel, "/")
	if len(parts) != 2 {
		return ModelRef{}, ErrInvalidRef
	}

	group := parts[0]
	model := parts[1]

	// Validate group and model are non-empty
	if group == "" || model == "" {
		return ModelRef{}, ErrInvalidRef
	}

	return ModelRef{
		Group:   group,
		Model:   model,
		Version: version,
	}, nil
}

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

	// BytesDownloaded is bytes actually fetched from network this session (excludes cache hits).
	// This is cumulative bytes from completed chunk downloads.
	BytesDownloaded int64

	// BytesInProgress is bytes currently being downloaded across all workers.
	// Use BytesDownloaded + BytesInProgress for smooth, accurate speed calculation.
	BytesInProgress int64

	// CurrentFile is the file being processed during extraction phase.
	CurrentFile string
}
