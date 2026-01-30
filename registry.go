package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// modelsIndex represents the models.json file from the remote registry.
// Structure: group → model → version → versionEntry
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
// The baseURL is normalized by removing any trailing slashes.
func newRegistryClient(baseURL string, client HTTPClient, logger Logger) *registryClient {
	return &registryClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: client,
		logger:     logger,
	}
}

// fetchModelsIndex fetches and parses the models.json index from the registry.
func (r *registryClient) fetchModelsIndex(ctx context.Context) (modelsIndex, error) {
	url := r.baseURL + "/models.json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models index: %w", ErrNetworkError)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching models index: status %d: %w", resp.StatusCode, ErrRegistryError)
	}

	var index modelsIndex
	if err := json.NewDecoder(resp.Body).Decode(&index); err != nil {
		return nil, fmt.Errorf("parsing models index: %w", ErrRegistryError)
	}

	return index, nil
}

// fetchManifest fetches and parses a manifest from data/<hash>.
func (r *registryClient) fetchManifest(ctx context.Context, hash string) (manifest, error) {
	url := r.baseURL + "/data/" + hash

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return manifest{}, fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return manifest{}, fmt.Errorf("fetching manifest %s: %w", hash, ErrNetworkError)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return manifest{}, fmt.Errorf("manifest %s: %w", hash, ErrModelNotFound)
	}

	if resp.StatusCode != http.StatusOK {
		return manifest{}, fmt.Errorf("fetching manifest %s: status %d: %w", hash, resp.StatusCode, ErrRegistryError)
	}

	var mf manifest
	if err := json.NewDecoder(resp.Body).Decode(&mf); err != nil {
		return manifest{}, fmt.Errorf("parsing manifest %s: %w", hash, ErrRegistryError)
	}

	return mf, nil
}

// fetchChunk fetches a chunk from data/<hash> and returns its contents.
// The caller is responsible for verifying the hash.
func (r *registryClient) fetchChunk(ctx context.Context, hash string) ([]byte, error) {
	return r.fetchChunkWithProgress(ctx, hash, nil)
}

// fetchChunkWithProgress fetches a chunk with optional progress reporting.
// The onProgress callback is called as bytes are read from the network.
// It receives the delta (bytes just read), not cumulative.
func (r *registryClient) fetchChunkWithProgress(ctx context.Context, hash string, onProgress func(delta int64)) ([]byte, error) {
	url := r.baseURL + "/data/" + hash

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching chunk %s: %w", hash, ErrNetworkError)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("chunk %s: %w", hash, ErrModelNotFound)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching chunk %s: status %d: %w", hash, resp.StatusCode, ErrRegistryError)
	}

	// Wrap body with progress reader if callback provided
	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &progressReader{reader: resp.Body, onProgress: onProgress}
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("reading chunk %s: %w", hash, ErrNetworkError)
	}

	return data, nil
}

// progressReader wraps an io.Reader and reports progress as bytes are read.
type progressReader struct {
	reader     io.Reader
	onProgress func(delta int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.reader.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.onProgress(int64(n))
	}
	return
}
