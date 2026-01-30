package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestVerifyHash(t *testing.T) {
	t.Run("matching hash returns nil", func(t *testing.T) {
		data := []byte("hello world")
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])

		err := verifyHash(data, hash)
		if err != nil {
			t.Errorf("verifyHash() error = %v, want nil", err)
		}
	})

	t.Run("mismatching hash returns ErrHashMismatch", func(t *testing.T) {
		data := []byte("hello world")
		wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"

		err := verifyHash(data, wrongHash)
		if !errors.Is(err, ErrHashMismatch) {
			t.Errorf("verifyHash() error = %v, want ErrHashMismatch", err)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		data := []byte{}
		h := sha256.Sum256(data)
		hash := hex.EncodeToString(h[:])

		err := verifyHash(data, hash)
		if err != nil {
			t.Errorf("verifyHash() error = %v, want nil", err)
		}
	})
}

func TestChunkCache(t *testing.T) {
	t.Run("put then get", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		hash := "abc123"
		data := []byte("chunk data")

		if err := cache.put(hash, data); err != nil {
			t.Fatalf("put() error = %v", err)
		}

		got, ok := cache.get(hash)
		if !ok {
			t.Fatal("get() returned false, want true")
		}
		if string(got) != string(data) {
			t.Errorf("get() = %q, want %q", string(got), string(data))
		}
	})

	t.Run("get missing returns false", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		_, ok := cache.get("nonexistent")
		if ok {
			t.Error("get() returned true for missing chunk, want false")
		}
	})

	t.Run("delete", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		hash := "todelete"
		data := []byte("data")

		if err := cache.put(hash, data); err != nil {
			t.Fatalf("put() error = %v", err)
		}

		if err := cache.delete(hash); err != nil {
			t.Fatalf("delete() error = %v", err)
		}

		_, ok := cache.get(hash)
		if ok {
			t.Error("get() returned true after delete, want false")
		}
	})

	t.Run("delete nonexistent is not an error", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		if err := cache.delete("nonexistent"); err != nil {
			t.Errorf("delete() error = %v, want nil", err)
		}
	})

	t.Run("cleanup removes old files", func(t *testing.T) {
		tmpDir := t.TempDir()
		cache := newChunkCache(tmpDir)

		// Create an old file directly
		oldFile := filepath.Join(tmpDir, "oldchunk")
		if err := os.WriteFile(oldFile, []byte("old"), 0644); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		// Set modification time to the past
		oldTime := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(oldFile, oldTime, oldTime); err != nil {
			t.Fatalf("Chtimes() error = %v", err)
		}

		// Create a new file
		newHash := "newchunk"
		if err := cache.put(newHash, []byte("new")); err != nil {
			t.Fatalf("put() error = %v", err)
		}

		// Cleanup with 24h max age
		if err := cache.cleanup(24 * time.Hour); err != nil {
			t.Fatalf("cleanup() error = %v", err)
		}

		// Old file should be gone
		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			t.Error("old file should have been removed")
		}

		// New file should still exist
		if _, ok := cache.get(newHash); !ok {
			t.Error("new chunk should still exist")
		}
	})

	t.Run("cleanup on nonexistent directory is not an error", func(t *testing.T) {
		cache := newChunkCache("/nonexistent/path")

		if err := cache.cleanup(24 * time.Hour); err != nil {
			t.Errorf("cleanup() error = %v, want nil", err)
		}
	})
}

// mockStorageForDownload implements storageInterface for download tests
type mockStorageForDownload struct {
	chunkCacheDir string
}

func (m *mockStorageForDownload) loadRegistry() (localRegistry, error) {
	return make(localRegistry), nil
}

func (m *mockStorageForDownload) saveRegistry(reg localRegistry) error {
	return nil
}

func (m *mockStorageForDownload) modelPath(ref ModelRef) string {
	return ""
}

func (m *mockStorageForDownload) chunkCachePath() string {
	return m.chunkCacheDir
}

func (m *mockStorageForDownload) ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

func (m *mockStorageForDownload) atomicWrite(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func (m *mockStorageForDownload) removeModelDir(ref ModelRef) error {
	return nil
}

func (m *mockStorageForDownload) saveManifest(ref ModelRef, mf manifest) error {
	return nil
}

func (m *mockStorageForDownload) removeChunkCache() error {
	return os.RemoveAll(m.chunkCacheDir)
}

// Ensure mockStorageForDownload implements storageInterface
var _ storageInterface = (*mockStorageForDownload)(nil)

func TestDownloadChunks(t *testing.T) {
	t.Run("successful download of multiple chunks", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create test data with valid hashes
		chunk1Data := []byte("chunk one data")
		chunk2Data := []byte("chunk two data")
		chunk3Data := []byte("chunk three data")

		h1 := sha256.Sum256(chunk1Data)
		h2 := sha256.Sum256(chunk2Data)
		h3 := sha256.Sum256(chunk3Data)

		hash1 := hex.EncodeToString(h1[:])
		hash2 := hex.EncodeToString(h2[:])
		hash3 := hex.EncodeToString(h3[:])

		chunks := []string{hash1, hash2, hash3}
		chunkData := map[string][]byte{
			hash1: chunk1Data,
			hash2: chunk2Data,
			hash3: chunk3Data,
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hash := filepath.Base(r.URL.Path)
			if data, ok := chunkData[hash]; ok {
				w.Write(data)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		registry := newRegistryClient(server.URL, server.Client(), nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		var progressCalls int32
		progressFn := func(completed, total int, bytesDownloaded, bytesInProgress int64) {
			atomic.AddInt32(&progressCalls, 1)
		}

		err := engine.downloadChunks(context.Background(), chunks, 2, progressFn)
		if err != nil {
			t.Fatalf("downloadChunks() error = %v", err)
		}

		if atomic.LoadInt32(&progressCalls) != 3 {
			t.Errorf("progressFn called %d times, want 3", progressCalls)
		}

		// Verify chunks are in cache
		cache := newChunkCache(storage.chunkCachePath())
		for _, hash := range chunks {
			if _, ok := cache.get(hash); !ok {
				t.Errorf("chunk %s not found in cache", hash[:8])
			}
		}
	})

	t.Run("cache is used on second call", func(t *testing.T) {
		tmpDir := t.TempDir()

		chunkData := []byte("cached chunk")
		h := sha256.Sum256(chunkData)
		hash := hex.EncodeToString(h[:])

		var serverHits int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&serverHits, 1)
			w.Write(chunkData)
		}))
		defer server.Close()

		registry := newRegistryClient(server.URL, server.Client(), nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		// First download
		err := engine.downloadChunks(context.Background(), []string{hash}, 1, nil)
		if err != nil {
			t.Fatalf("first downloadChunks() error = %v", err)
		}

		if atomic.LoadInt32(&serverHits) != 1 {
			t.Errorf("server hits = %d, want 1", serverHits)
		}

		// Second download should use cache
		err = engine.downloadChunks(context.Background(), []string{hash}, 1, nil)
		if err != nil {
			t.Fatalf("second downloadChunks() error = %v", err)
		}

		if atomic.LoadInt32(&serverHits) != 1 {
			t.Errorf("server hits after second call = %d, want 1 (should use cache)", serverHits)
		}
	})

	t.Run("hash mismatch error", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Server returns data that doesn't match the hash
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("wrong data"))
		}))
		defer server.Close()

		registry := newRegistryClient(server.URL, server.Client(), nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		hash := "0000000000000000000000000000000000000000000000000000000000000000"
		err := engine.downloadChunks(context.Background(), []string{hash}, 1, nil)

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrHashMismatch) {
			t.Errorf("expected ErrHashMismatch, got %v", err)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Server that blocks
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(5 * time.Second)
			w.Write([]byte("data"))
		}))
		defer server.Close()

		registry := newRegistryClient(server.URL, server.Client(), nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after a short delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		hash := "0000000000000000000000000000000000000000000000000000000000000000"
		err := engine.downloadChunks(ctx, []string{hash}, 1, nil)

		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty chunks list", func(t *testing.T) {
		tmpDir := t.TempDir()

		registry := newRegistryClient("http://unused", http.DefaultClient, nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		err := engine.downloadChunks(context.Background(), []string{}, 1, nil)
		if err != nil {
			t.Errorf("downloadChunks() error = %v, want nil", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create and immediately close server
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()

		registry := newRegistryClient(server.URL, server.Client(), nil)
		storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
		engine := newDownloadEngine(registry, storage, nil)

		hash := "0000000000000000000000000000000000000000000000000000000000000000"
		err := engine.downloadChunks(context.Background(), []string{hash}, 1, nil)

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNetworkError) {
			t.Errorf("expected ErrNetworkError, got %v", err)
		}
	})
}

func TestDownloadEngineWithLogger(t *testing.T) {
	tmpDir := t.TempDir()

	chunkData := []byte("logged chunk")
	h := sha256.Sum256(chunkData)
	hash := hex.EncodeToString(h[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(chunkData)
	}))
	defer server.Close()

	// Simple logger that counts calls
	var debugCalls int32
	logger := &downloadTestLogger{
		debugFn: func(msg string, kv ...any) {
			atomic.AddInt32(&debugCalls, 1)
		},
	}

	registry := newRegistryClient(server.URL, server.Client(), nil)
	storage := &mockStorageForDownload{chunkCacheDir: filepath.Join(tmpDir, ".chunks")}
	engine := newDownloadEngine(registry, storage, logger)

	err := engine.downloadChunks(context.Background(), []string{hash}, 1, nil)
	if err != nil {
		t.Fatalf("downloadChunks() error = %v", err)
	}

	if atomic.LoadInt32(&debugCalls) == 0 {
		t.Error("expected logger.Debug to be called")
	}
}

// downloadTestLogger is a simple Logger implementation for download tests
type downloadTestLogger struct {
	debugFn func(msg string, kv ...any)
}

func (l *downloadTestLogger) Debug(msg string, keysAndValues ...any) {
	if l.debugFn != nil {
		l.debugFn(msg, keysAndValues...)
	}
}

func (l *downloadTestLogger) Info(msg string, keysAndValues ...any)  {}
func (l *downloadTestLogger) Warn(msg string, keysAndValues ...any)  {}
func (l *downloadTestLogger) Error(msg string, keysAndValues ...any) {}

// Ensure downloadTestLogger implements Logger
var _ Logger = (*downloadTestLogger)(nil)
