package models

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnvVarName(t *testing.T) {
	tests := []struct {
		appName string
		want    string
	}{
		{"xprim", "XPRIM_MODELS_DIR"},
		{"myapp", "MYAPP_MODELS_DIR"},
		{"MyApp", "MYAPP_MODELS_DIR"},
		{"my-app", "MY-APP_MODELS_DIR"},
	}

	for _, tt := range tests {
		t.Run(tt.appName, func(t *testing.T) {
			got := envVarName(tt.appName)
			if got != tt.want {
				t.Errorf("envVarName(%q) = %q, want %q", tt.appName, got, tt.want)
			}
		})
	}
}

func TestNewStorageWithDataDir(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		AppName: "testapp",
		DataDir: tmpDir,
	}

	s, err := newStorage(cfg)
	if err != nil {
		t.Fatalf("newStorage() error = %v", err)
	}

	if s.baseDir != tmpDir {
		t.Errorf("baseDir = %q, want %q", s.baseDir, tmpDir)
	}
}

func TestNewStorageWithEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	envName := envVarName("testenvapp")

	// Set env var
	os.Setenv(envName, tmpDir)
	defer os.Unsetenv(envName)

	cfg := Config{
		AppName: "testenvapp",
		DataDir: "/should/be/ignored",
	}

	s, err := newStorage(cfg)
	if err != nil {
		t.Fatalf("newStorage() error = %v", err)
	}

	if s.baseDir != tmpDir {
		t.Errorf("baseDir = %q, want %q (env var should take priority)", s.baseDir, tmpDir)
	}
}

func TestAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	testFile := filepath.Join(tmpDir, "test.txt")
	testData := []byte("hello world")

	// Write file
	if err := s.atomicWrite(testFile, testData); err != nil {
		t.Fatalf("atomicWrite() error = %v", err)
	}

	// Verify file exists and has correct content
	got, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(got) != string(testData) {
		t.Errorf("file content = %q, want %q", string(got), string(testData))
	}

	// Verify temp file doesn't exist (atomic write should clean up)
	tmpFile := testFile + ".tmp"
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Errorf("temp file %q should not exist after atomic write", tmpFile)
	}
}

func TestAtomicWriteCreatesDir(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	// Write to a nested path that doesn't exist
	testFile := filepath.Join(tmpDir, "nested", "dir", "test.txt")
	testData := []byte("nested data")

	if err := s.atomicWrite(testFile, testData); err != nil {
		t.Fatalf("atomicWrite() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("file should exist after atomicWrite")
	}
}

func TestLoadRegistryMissing(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	reg, err := s.loadRegistry()
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}

	if reg == nil {
		t.Error("loadRegistry() should return non-nil empty registry")
	}

	if len(reg) != 0 {
		t.Errorf("loadRegistry() returned non-empty registry: %v", reg)
	}
}

func TestLoadSaveRegistryRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	// Create test registry
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip
	reg := localRegistry{
		"fast-whisper": {
			"tiny": {
				"fp16": {
					ManifestHash: "abc123",
					TotalSize:    1024,
					FileCount:    2,
					InstalledAt:  now,
				},
			},
		},
	}

	// Save registry
	if err := s.saveRegistry(reg); err != nil {
		t.Fatalf("saveRegistry() error = %v", err)
	}

	// Load registry
	loaded, err := s.loadRegistry()
	if err != nil {
		t.Fatalf("loadRegistry() error = %v", err)
	}

	// Verify content
	entry, ok := loaded["fast-whisper"]["tiny"]["fp16"]
	if !ok {
		t.Fatal("expected entry not found in loaded registry")
	}

	if entry.ManifestHash != "abc123" {
		t.Errorf("ManifestHash = %q, want %q", entry.ManifestHash, "abc123")
	}
	if entry.TotalSize != 1024 {
		t.Errorf("TotalSize = %d, want %d", entry.TotalSize, 1024)
	}
	if entry.FileCount != 2 {
		t.Errorf("FileCount = %d, want %d", entry.FileCount, 2)
	}
	if !entry.InstalledAt.Equal(now) {
		t.Errorf("InstalledAt = %v, want %v", entry.InstalledAt, now)
	}
}

func TestModelPath(t *testing.T) {
	s := &storage{baseDir: "/data/models"}

	ref := ModelRef{
		Group:   "fast-whisper",
		Model:   "tiny",
		Version: "fp16",
	}

	got := s.modelPath(ref)
	want := filepath.Join("/data/models", "fast-whisper", "tiny", "fp16")

	if got != want {
		t.Errorf("modelPath() = %q, want %q", got, want)
	}
}

func TestChunkCachePath(t *testing.T) {
	s := &storage{baseDir: "/data/models"}

	got := s.chunkCachePath()
	want := filepath.Join("/data/models", ".chunks")

	if got != want {
		t.Errorf("chunkCachePath() = %q, want %q", got, want)
	}
}

func TestEnsureDir(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	newDir := filepath.Join(tmpDir, "new", "nested", "dir")

	if err := s.ensureDir(newDir); err != nil {
		t.Fatalf("ensureDir() error = %v", err)
	}

	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if !info.IsDir() {
		t.Error("ensureDir() should create a directory")
	}
}

func TestRemoveModelDir(t *testing.T) {
	tmpDir := t.TempDir()
	s := &storage{baseDir: tmpDir}

	ref := ModelRef{
		Group:   "test-group",
		Model:   "test-model",
		Version: "v1",
	}

	// Create the model directory with a file
	modelDir := s.modelPath(ref)
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	testFile := filepath.Join(modelDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Remove the model directory
	if err := s.removeModelDir(ref); err != nil {
		t.Fatalf("removeModelDir() error = %v", err)
	}

	// Verify directory no longer exists
	if _, err := os.Stat(modelDir); !os.IsNotExist(err) {
		t.Error("model directory should not exist after removeModelDir()")
	}
}

func TestGetDefaultDataDir(t *testing.T) {
	// This test verifies getDefaultDataDir works on the current platform
	dir, err := getDefaultDataDir("testapp")
	if err != nil {
		t.Fatalf("getDefaultDataDir() error = %v", err)
	}

	if dir == "" {
		t.Error("getDefaultDataDir() returned empty string")
	}

	// Should end with the app name and "models"
	if !filepath.IsAbs(dir) {
		t.Errorf("getDefaultDataDir() should return absolute path, got %q", dir)
	}
}
