package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestChunkStreamReader(t *testing.T) {
	t.Run("single chunk read", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		data := []byte("hello world")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])

		if err := cache.put(hash, data); err != nil {
			t.Fatalf("cache.put() error = %v", err)
		}

		reader := newChunkStreamReader([]string{hash}, cache)

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		if string(result) != string(data) {
			t.Errorf("got %q, want %q", string(result), string(data))
		}
	})

	t.Run("multiple chunks read sequentially", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		chunk1 := []byte("hello ")
		chunk2 := []byte("world")
		chunk3 := []byte("!")

		h1 := sha256.Sum256(chunk1)
		h2 := sha256.Sum256(chunk2)
		h3 := sha256.Sum256(chunk3)

		hash1 := hex.EncodeToString(h1[:])
		hash2 := hex.EncodeToString(h2[:])
		hash3 := hex.EncodeToString(h3[:])

		cache.put(hash1, chunk1)
		cache.put(hash2, chunk2)
		cache.put(hash3, chunk3)

		reader := newChunkStreamReader([]string{hash1, hash2, hash3}, cache)

		result, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}

		expected := "hello world!"
		if string(result) != expected {
			t.Errorf("got %q, want %q", string(result), expected)
		}
	})

	t.Run("empty chunks returns EOF immediately", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		reader := newChunkStreamReader([]string{}, cache)

		buf := make([]byte, 10)
		n, err := reader.Read(buf)

		if err != io.EOF {
			t.Errorf("Read() error = %v, want io.EOF", err)
		}
		if n != 0 {
			t.Errorf("Read() n = %d, want 0", n)
		}
	})

	t.Run("partial reads work correctly", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		data := []byte("0123456789")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])

		cache.put(hash, data)

		reader := newChunkStreamReader([]string{hash}, cache)

		// Read 3 bytes at a time
		buf := make([]byte, 3)
		var result []byte

		for {
			n, err := reader.Read(buf)
			result = append(result, buf[:n]...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
		}

		if string(result) != string(data) {
			t.Errorf("got %q, want %q", string(result), string(data))
		}
	})

	t.Run("missing chunk returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		reader := newChunkStreamReader([]string{"nonexistent"}, cache)

		buf := make([]byte, 10)
		_, err := reader.Read(buf)

		if err == nil {
			t.Error("Read() should return error for missing chunk")
		}
	})

	t.Run("totalRead tracks bytes correctly", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		chunk1 := []byte("hello")
		chunk2 := []byte("world")

		h1 := sha256.Sum256(chunk1)
		h2 := sha256.Sum256(chunk2)
		hash1 := hex.EncodeToString(h1[:])
		hash2 := hex.EncodeToString(h2[:])

		cache.put(hash1, chunk1)
		cache.put(hash2, chunk2)

		reader := newChunkStreamReader([]string{hash1, hash2}, cache)

		io.ReadAll(reader)

		expectedTotal := int64(len(chunk1) + len(chunk2))
		if reader.totalRead != expectedTotal {
			t.Errorf("totalRead = %d, want %d", reader.totalRead, expectedTotal)
		}
	})
}

// mockStorageForReconstruct implements storageInterface for reconstruct tests
type mockStorageForReconstruct struct {
	baseDir string
}

func (m *mockStorageForReconstruct) loadRegistry() (localRegistry, error) {
	return make(localRegistry), nil
}

func (m *mockStorageForReconstruct) saveRegistry(reg localRegistry) error {
	return nil
}

func (m *mockStorageForReconstruct) modelPath(ref ModelRef) string {
	return filepath.Join(m.baseDir, ref.Group, ref.Model, ref.Version)
}

func (m *mockStorageForReconstruct) chunkCachePath() string {
	return filepath.Join(m.baseDir, ".chunks")
}

func (m *mockStorageForReconstruct) ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (m *mockStorageForReconstruct) atomicWrite(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func (m *mockStorageForReconstruct) removeModelDir(ref ModelRef) error {
	return os.RemoveAll(m.modelPath(ref))
}

func (m *mockStorageForReconstruct) saveManifest(ref ModelRef, mf manifest) error {
	return nil
}

func (m *mockStorageForReconstruct) removeChunkCache() error {
	return os.RemoveAll(m.chunkCachePath())
}

var _ storageInterface = (*mockStorageForReconstruct)(nil)

func TestFileReconstructor(t *testing.T) {
	t.Run("reconstruct files from cached chunks", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, ".chunks")
		cache := newChunkCache(cacheDir)
		storage := &mockStorageForReconstruct{baseDir: tmpDir}

		// Create test data - two files spanning multiple chunks
		// File 1: "Hello, " (7 bytes)
		// File 2: "World!" (6 bytes)
		// Total: 13 bytes in 2 chunks
		chunk1 := []byte("Hello, Wo")   // 9 bytes
		chunk2 := []byte("rld!")        // 4 bytes

		h1 := sha256.Sum256(chunk1)
		h2 := sha256.Sum256(chunk2)
		hash1 := hex.EncodeToString(h1[:])
		hash2 := hex.EncodeToString(h2[:])

		cache.put(hash1, chunk1)
		cache.put(hash2, chunk2)

		mf := manifest{
			TotalSize: 13,
			ChunkSize: 10,
			Chunks:    []string{hash1, hash2},
			Files: []manifestFile{
				{Path: "greeting.txt", Size: 7},  // "Hello, "
				{Path: "name.txt", Size: 6},      // "World!"
			},
		}

		ref := ModelRef{Group: "test", Model: "model", Version: "v1"}

		reconstructor := newFileReconstructor(cache, storage)
		err := reconstructor.reconstruct(context.Background(), mf, ref, nil)
		if err != nil {
			t.Fatalf("reconstruct() error = %v", err)
		}

		// Verify files
		modelDir := storage.modelPath(ref)

		content1, err := os.ReadFile(filepath.Join(modelDir, "greeting.txt"))
		if err != nil {
			t.Fatalf("reading greeting.txt: %v", err)
		}
		if string(content1) != "Hello, " {
			t.Errorf("greeting.txt = %q, want %q", string(content1), "Hello, ")
		}

		content2, err := os.ReadFile(filepath.Join(modelDir, "name.txt"))
		if err != nil {
			t.Fatalf("reading name.txt: %v", err)
		}
		if string(content2) != "World!" {
			t.Errorf("name.txt = %q, want %q", string(content2), "World!")
		}
	})

	t.Run("reconstruct with nested directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, ".chunks")
		cache := newChunkCache(cacheDir)
		storage := &mockStorageForReconstruct{baseDir: tmpDir}

		data := []byte("nested file content")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])
		cache.put(hash, data)

		mf := manifest{
			TotalSize: int64(len(data)),
			ChunkSize: 1024,
			Chunks:    []string{hash},
			Files: []manifestFile{
				{Path: "subdir/nested/file.txt", Size: int64(len(data))},
			},
		}

		ref := ModelRef{Group: "test", Model: "model", Version: "v1"}

		reconstructor := newFileReconstructor(cache, storage)
		err := reconstructor.reconstruct(context.Background(), mf, ref, nil)
		if err != nil {
			t.Fatalf("reconstruct() error = %v", err)
		}

		modelDir := storage.modelPath(ref)
		content, err := os.ReadFile(filepath.Join(modelDir, "subdir/nested/file.txt"))
		if err != nil {
			t.Fatalf("reading nested file: %v", err)
		}
		if string(content) != string(data) {
			t.Errorf("nested file content = %q, want %q", string(content), string(data))
		}
	})

	t.Run("progressFn is called for each file", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, ".chunks")
		cache := newChunkCache(cacheDir)
		storage := &mockStorageForReconstruct{baseDir: tmpDir}

		data := []byte("test data for progress")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])
		cache.put(hash, data)

		mf := manifest{
			TotalSize: int64(len(data)),
			ChunkSize: 1024,
			Chunks:    []string{hash},
			Files: []manifestFile{
				{Path: "file1.txt", Size: 5},
				{Path: "file2.txt", Size: 5},
				{Path: "file3.txt", Size: int64(len(data) - 10)},
			},
		}

		ref := ModelRef{Group: "test", Model: "model", Version: "v1"}

		var progressCalls int32
		var lastFile string
		progressFn := func(currentFile string) {
			atomic.AddInt32(&progressCalls, 1)
			lastFile = currentFile
		}

		reconstructor := newFileReconstructor(cache, storage)
		err := reconstructor.reconstruct(context.Background(), mf, ref, progressFn)
		if err != nil {
			t.Fatalf("reconstruct() error = %v", err)
		}

		if atomic.LoadInt32(&progressCalls) != 3 {
			t.Errorf("progressFn called %d times, want 3", progressCalls)
		}
		if lastFile != "file3.txt" {
			t.Errorf("last file = %q, want %q", lastFile, "file3.txt")
		}
	})

	t.Run("context cancellation stops reconstruction", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, ".chunks")
		cache := newChunkCache(cacheDir)
		storage := &mockStorageForReconstruct{baseDir: tmpDir}

		data := []byte("some data")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])
		cache.put(hash, data)

		mf := manifest{
			TotalSize: int64(len(data)),
			ChunkSize: 1024,
			Chunks:    []string{hash},
			Files: []manifestFile{
				{Path: "file1.txt", Size: 4},
				{Path: "file2.txt", Size: 5},
			},
		}

		ref := ModelRef{Group: "test", Model: "model", Version: "v1"}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		reconstructor := newFileReconstructor(cache, storage)
		err := reconstructor.reconstruct(ctx, mf, ref, nil)

		if err == nil {
			t.Error("expected error with cancelled context")
		}
	})

	t.Run("empty manifest succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, ".chunks")
		cache := newChunkCache(cacheDir)
		storage := &mockStorageForReconstruct{baseDir: tmpDir}

		mf := manifest{
			TotalSize: 0,
			ChunkSize: 1024,
			Chunks:    []string{},
			Files:     []manifestFile{},
		}

		ref := ModelRef{Group: "test", Model: "model", Version: "v1"}

		reconstructor := newFileReconstructor(cache, storage)
		err := reconstructor.reconstruct(context.Background(), mf, ref, nil)
		if err != nil {
			t.Errorf("reconstruct() error = %v, want nil", err)
		}
	})
}
