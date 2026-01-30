package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// mockStorageForManager implements storageInterface for manager tests
type mockStorageForManager struct {
	mu       sync.Mutex
	baseDir  string
	appName  string
	registry localRegistry
	removed  []ModelRef
}

func newMockStorageForManager(baseDir string) *mockStorageForManager {
	return &mockStorageForManager{
		baseDir:  baseDir,
		appName:  "test",
		registry: make(localRegistry),
	}
}

func (m *mockStorageForManager) loadRegistry() (localRegistry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Return a copy
	copy := make(localRegistry)
	for g, gm := range m.registry {
		copy[g] = make(map[string]map[string]installedModelEntry)
		for model, versions := range gm {
			copy[g][model] = make(map[string]installedModelEntry)
			for v, e := range versions {
				copy[g][model][v] = e
			}
		}
	}
	return copy, nil
}

func (m *mockStorageForManager) saveRegistry(reg localRegistry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registry = reg
	return nil
}

func (m *mockStorageForManager) modelPath(ref ModelRef) string {
	return filepath.Join(m.baseDir, ref.Group, ref.Model, ref.Version)
}

func (m *mockStorageForManager) chunkCachePath() string {
	return filepath.Join(m.baseDir, ".chunks")
}

func (m *mockStorageForManager) ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (m *mockStorageForManager) atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *mockStorageForManager) removeModelDir(ref ModelRef) error {
	m.mu.Lock()
	m.removed = append(m.removed, ref)
	m.mu.Unlock()
	return os.RemoveAll(m.modelPath(ref))
}

func (m *mockStorageForManager) saveManifest(ref ModelRef, mf manifest) error {
	metaDir := filepath.Join(m.modelPath(ref), "."+m.appName)
	if err := m.ensureDir(metaDir); err != nil {
		return err
	}
	path := filepath.Join(metaDir, "manifest.json")
	data, err := json.Marshal(mf)
	if err != nil {
		return err
	}
	return m.atomicWrite(path, data)
}

func (m *mockStorageForManager) removeChunkCache() error {
	return os.RemoveAll(m.chunkCachePath())
}

func (m *mockStorageForManager) addInstalled(ref ModelRef, hash string, size int64, fileCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.registry[ref.Group] == nil {
		m.registry[ref.Group] = make(map[string]map[string]installedModelEntry)
	}
	if m.registry[ref.Group][ref.Model] == nil {
		m.registry[ref.Group][ref.Model] = make(map[string]installedModelEntry)
	}
	m.registry[ref.Group][ref.Model][ref.Version] = installedModelEntry{
		ManifestHash: hash,
		TotalSize:    size,
		FileCount:    fileCount,
		InstalledAt:  time.Now(),
	}
}

var _ storageInterface = (*mockStorageForManager)(nil)

// makeIndex creates a modelsIndex in the format expected by the registry.
// Takes pairs of (group, model, version, hash).
// makeIndex creates a modelsIndex in the format expected by the registry.
// Takes groups of 4 strings: group, model, version, hash.
func makeIndex(entries ...string) modelsIndex {
	if len(entries)%4 != 0 {
		panic("makeIndex requires entries in groups of 4: group, model, version, hash")
	}

	index := make(modelsIndex)

	for i := 0; i < len(entries); i += 4 {
		groupName := entries[i]
		modelName := entries[i+1]
		versionName := entries[i+2]
		hash := entries[i+3]

		if _, ok := index[groupName]; !ok {
			index[groupName] = make(map[string]map[string]versionEntry)
		}

		if _, ok := index[groupName][modelName]; !ok {
			index[groupName][modelName] = make(map[string]versionEntry)
		}

		index[groupName][modelName][versionName] = versionEntry{
			Hash: hash,
		}
	}

	return index
}

func TestListInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	// Add some installed models
	storage.addInstalled(ModelRef{Group: "fast-whisper", Model: "tiny", Version: "fp16"}, "hash1", 1000, 2)
	storage.addInstalled(ModelRef{Group: "fast-whisper", Model: "small", Version: "int8"}, "hash2", 2000, 3)
	storage.addInstalled(ModelRef{Group: "llama", Model: "7b", Version: "q4"}, "hash3", 3000, 1)

	mgr := &manager{
		storage: storage,
	}

	models, err := mgr.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled() error = %v", err)
	}

	if len(models) != 3 {
		t.Errorf("ListInstalled() returned %d models, want 3", len(models))
	}
}

func TestListInstalledEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	mgr := &manager{
		storage: storage,
	}

	models, err := mgr.ListInstalled(context.Background())
	if err != nil {
		t.Fatalf("ListInstalled() error = %v", err)
	}

	if len(models) != 0 {
		t.Errorf("ListInstalled() returned %d models, want 0", len(models))
	}
}

func TestGetInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	ref := ModelRef{Group: "fast-whisper", Model: "tiny", Version: "fp16"}
	storage.addInstalled(ref, "abc123", 1024, 2)

	mgr := &manager{
		storage: storage,
	}

	model, err := mgr.GetInstalled(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetInstalled() error = %v", err)
	}

	if model.ManifestHash != "abc123" {
		t.Errorf("ManifestHash = %q, want %q", model.ManifestHash, "abc123")
	}
	if model.TotalSize != 1024 {
		t.Errorf("TotalSize = %d, want %d", model.TotalSize, 1024)
	}
}

func TestGetInstalledNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	mgr := &manager{
		storage: storage,
	}

	ref := ModelRef{Group: "nonexistent", Model: "model", Version: "v1"}
	_, err := mgr.GetInstalled(context.Background(), ref)

	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("GetInstalled() error = %v, want ErrNotInstalled", err)
	}
}

// createTestServer creates a test HTTP server for manager tests.
// manifests is keyed by manifest hash (content-addressed).
// chunks is keyed by chunk hash.
func createTestServer(t *testing.T, modelsIndex modelsIndex, manifests map[string]manifest, chunks map[string][]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models.json" {
			json.NewEncoder(w).Encode(modelsIndex)
			return
		}

		// Check for manifest or chunk in /data/
		if len(r.URL.Path) > 6 && r.URL.Path[:6] == "/data/" {
			hash := r.URL.Path[6:]

			if mf, ok := manifests[hash]; ok {
				json.NewEncoder(w).Encode(mf)
				return
			}

			if data, ok := chunks[hash]; ok {
				w.Write(data)
				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestGetRemote(t *testing.T) {
	index := makeIndex("fast-whisper", "tiny", "fp16", "manifest123")

	server := createTestServer(t, index, nil, nil)
	defer server.Close()

	mgr := &manager{
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	ref := ModelRef{Group: "fast-whisper", Model: "tiny", Version: "fp16"}
	model, err := mgr.GetRemote(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetRemote() error = %v", err)
	}

	if model.ManifestHash != "manifest123" {
		t.Errorf("ManifestHash = %q, want %q", model.ManifestHash, "manifest123")
	}
}

func TestGetRemoteNotFound(t *testing.T) {
	index := make(modelsIndex)

	server := createTestServer(t, index, nil, nil)
	defer server.Close()

	mgr := &manager{
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	ref := ModelRef{Group: "nonexistent", Model: "model", Version: "v1"}
	_, err := mgr.GetRemote(context.Background(), ref)

	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("GetRemote() error = %v, want ErrModelNotFound", err)
	}
}

func TestListRemote(t *testing.T) {
	index := makeIndex(
		"fast-whisper", "tiny", "fp16", "hash1",
		"fast-whisper", "tiny", "int8", "hash2",
		"llama", "7b", "q4", "hash3",
	)

	server := createTestServer(t, index, nil, nil)
	defer server.Close()

	mgr := &manager{
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	models, err := mgr.ListRemote(context.Background())
	if err != nil {
		t.Fatalf("ListRemote() error = %v", err)
	}

	if len(models) != 3 {
		t.Errorf("ListRemote() returned %d models, want 3", len(models))
	}
}

func TestPull(t *testing.T) {
	tmpDir := t.TempDir()

	// Create chunk data
	chunkData := []byte("test model file content here!")
	h := sha256.Sum256(chunkData)
	chunkHash := hex.EncodeToString(h[:])

	manifestHash := "manifest_hash_123"
	mf := manifest{
		TotalSize: int64(len(chunkData)),
		ChunkSize: 1024,
		Chunks:    []string{chunkHash},
		Files: []manifestFile{
			{Path: "model.bin", Size: int64(len(chunkData))},
		},
	}

	index := makeIndex("test", "model", "v1", manifestHash)

	server := createTestServer(t, index, map[string]manifest{manifestHash: mf}, map[string][]byte{chunkHash: chunkData})
	defer server.Close()

	storage := newMockStorageForManager(tmpDir)
	mgr := &manager{
		storage:  storage,
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	err := mgr.Pull(context.Background(), ref)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	// Verify model is now installed
	installed, err := mgr.GetInstalled(context.Background(), ref)
	if err != nil {
		t.Fatalf("GetInstalled() after Pull error = %v", err)
	}

	if installed.ManifestHash != manifestHash {
		t.Errorf("installed ManifestHash = %q, want %q", installed.ManifestHash, manifestHash)
	}

	// Verify file was created
	modelPath := filepath.Join(storage.modelPath(ref), "model.bin")
	content, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("reading model file: %v", err)
	}

	if string(content) != string(chunkData) {
		t.Errorf("model file content = %q, want %q", string(content), string(chunkData))
	}
}

func TestPullAlreadyInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	manifestHash := "manifest_hash_123"
	index := makeIndex("test", "model", "v1", manifestHash)

	server := createTestServer(t, index, nil, nil)
	defer server.Close()

	storage := newMockStorageForManager(tmpDir)
	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	storage.addInstalled(ref, manifestHash, 1024, 1) // Same hash

	mgr := &manager{
		storage:  storage,
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	err := mgr.Pull(context.Background(), ref)

	if !errors.Is(err, ErrAlreadyInstalled) {
		t.Errorf("Pull() error = %v, want ErrAlreadyInstalled", err)
	}
}

func TestPullWithForce(t *testing.T) {
	tmpDir := t.TempDir()

	chunkData := []byte("new content")
	h := sha256.Sum256(chunkData)
	chunkHash := hex.EncodeToString(h[:])

	manifestHash := "manifest_hash_123"
	mf := manifest{
		TotalSize: int64(len(chunkData)),
		ChunkSize: 1024,
		Chunks:    []string{chunkHash},
		Files: []manifestFile{
			{Path: "model.bin", Size: int64(len(chunkData))},
		},
	}

	index := makeIndex("test", "model", "v1", manifestHash)

	server := createTestServer(t, index, map[string]manifest{manifestHash: mf}, map[string][]byte{chunkHash: chunkData})
	defer server.Close()

	storage := newMockStorageForManager(tmpDir)
	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	storage.addInstalled(ref, manifestHash, 1024, 1) // Same hash, but we force

	mgr := &manager{
		storage:  storage,
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	err := mgr.Pull(context.Background(), ref, WithForce())
	if err != nil {
		t.Fatalf("Pull() with force error = %v", err)
	}
}

func TestRemove(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	storage.addInstalled(ref, "hash", 1024, 1)

	// Create the model directory
	modelDir := storage.modelPath(ref)
	os.MkdirAll(modelDir, 0755)
	os.WriteFile(filepath.Join(modelDir, "test.txt"), []byte("test"), 0644)

	mgr := &manager{
		storage: storage,
	}

	err := mgr.Remove(context.Background(), ref)
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Verify model is no longer installed
	_, err = mgr.GetInstalled(context.Background(), ref)
	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("GetInstalled() after Remove error = %v, want ErrNotInstalled", err)
	}
}

func TestRemoveNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	mgr := &manager{
		storage: storage,
	}

	ref := ModelRef{Group: "nonexistent", Model: "model", Version: "v1"}
	err := mgr.Remove(context.Background(), ref)

	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Remove() error = %v, want ErrNotInstalled", err)
	}
}

func TestPath(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	storage.addInstalled(ref, "hash", 1024, 1)

	mgr := &manager{
		storage: storage,
	}

	path, err := mgr.Path(context.Background(), ref)
	if err != nil {
		t.Fatalf("Path() error = %v", err)
	}

	expected := storage.modelPath(ref)
	if path != expected {
		t.Errorf("Path() = %q, want %q", path, expected)
	}
}

func TestPathNotInstalled(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	mgr := &manager{
		storage: storage,
	}

	ref := ModelRef{Group: "nonexistent", Model: "model", Version: "v1"}
	_, err := mgr.Path(context.Background(), ref)

	if !errors.Is(err, ErrNotInstalled) {
		t.Errorf("Path() error = %v, want ErrNotInstalled", err)
	}
}

func TestCheckUpdate(t *testing.T) {
	tmpDir := t.TempDir()

	installedHash := "old_hash"
	remoteHash := "new_hash"

	index := makeIndex("test", "model", "v1", remoteHash)

	server := createTestServer(t, index, nil, nil)
	defer server.Close()

	storage := newMockStorageForManager(tmpDir)
	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	storage.addInstalled(ref, installedHash, 1024, 1)

	mgr := &manager{
		storage:  storage,
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	gotInstalled, gotRemote, err := mgr.CheckUpdate(context.Background(), ref)
	if err != nil {
		t.Fatalf("CheckUpdate() error = %v", err)
	}

	if gotInstalled != installedHash {
		t.Errorf("installedHash = %q, want %q", gotInstalled, installedHash)
	}
	if gotRemote != remoteHash {
		t.Errorf("remoteHash = %q, want %q", gotRemote, remoteHash)
	}

	// Hashes differ, so update is available
	if gotInstalled == gotRemote {
		t.Error("expected different hashes indicating update available")
	}
}

func TestPullWithProgress(t *testing.T) {
	tmpDir := t.TempDir()

	chunkData := []byte("chunk content")
	h := sha256.Sum256(chunkData)
	chunkHash := hex.EncodeToString(h[:])

	manifestHash := "manifest_hash"
	mf := manifest{
		TotalSize: int64(len(chunkData)),
		ChunkSize: 1024,
		Chunks:    []string{chunkHash},
		Files: []manifestFile{
			{Path: "file.txt", Size: int64(len(chunkData))},
		},
	}

	index := makeIndex("test", "model", "v1", manifestHash)

	server := createTestServer(t, index, map[string]manifest{manifestHash: mf}, map[string][]byte{chunkHash: chunkData})
	defer server.Close()

	storage := newMockStorageForManager(tmpDir)
	mgr := &manager{
		storage:  storage,
		registry: newRegistryClient(server.URL, server.Client(), nil),
	}

	var phases []string
	progressFn := func(p PullProgress) {
		phases = append(phases, p.Phase)
	}

	ref := ModelRef{Group: "test", Model: "model", Version: "v1"}
	err := mgr.Pull(context.Background(), ref, WithProgress(progressFn))
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	// Should have seen manifest, chunks, and extracting phases
	hasManifest := false
	hasChunks := false
	hasExtracting := false
	for _, p := range phases {
		switch p {
		case "manifest":
			hasManifest = true
		case "chunks":
			hasChunks = true
		case "extracting":
			hasExtracting = true
		}
	}

	if !hasManifest || !hasChunks || !hasExtracting {
		t.Errorf("phases = %v, want manifest, chunks, and extracting", phases)
	}
}

func TestPruneCache(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	// Create chunk cache directory with some files
	chunkDir := storage.chunkCachePath()
	os.MkdirAll(chunkDir, 0755)
	os.WriteFile(filepath.Join(chunkDir, "chunk1.bin"), []byte("data1"), 0644)
	os.WriteFile(filepath.Join(chunkDir, "chunk2.bin"), []byte("data2"), 0644)

	mgr := &manager{
		storage: storage,
	}

	// Verify chunk cache exists
	if _, err := os.Stat(chunkDir); os.IsNotExist(err) {
		t.Fatal("chunk cache should exist before prune")
	}

	// Prune the cache
	err := mgr.PruneCache(context.Background())
	if err != nil {
		t.Fatalf("PruneCache() error = %v", err)
	}

	// Verify chunk cache is gone
	if _, err := os.Stat(chunkDir); !os.IsNotExist(err) {
		t.Error("chunk cache should not exist after prune")
	}
}

func TestPruneCacheEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	storage := newMockStorageForManager(tmpDir)

	mgr := &manager{
		storage: storage,
	}

	// Prune when no cache exists should not error
	err := mgr.PruneCache(context.Background())
	if err != nil {
		t.Fatalf("PruneCache() on empty cache error = %v", err)
	}
}
