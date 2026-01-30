package models

import (
	"net/http"
	"strings"
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Run("empty AppName returns error", func(t *testing.T) {
		cfg := Config{
			AppName:     "",
			RegistryURL: "https://example.com",
		}

		_, err := NewManager(cfg)
		if err == nil {
			t.Fatal("expected error for empty AppName")
		}
		if !strings.Contains(err.Error(), "AppName") {
			t.Errorf("error should mention AppName: %v", err)
		}
	})

	t.Run("empty RegistryURL returns error", func(t *testing.T) {
		cfg := Config{
			AppName:     "testapp",
			RegistryURL: "",
		}

		_, err := NewManager(cfg)
		if err == nil {
			t.Fatal("expected error for empty RegistryURL")
		}
		if !strings.Contains(err.Error(), "RegistryURL") {
			t.Errorf("error should mention RegistryURL: %v", err)
		}
	})

	t.Run("valid config succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := Config{
			AppName:     "testapp",
			RegistryURL: "https://example.com",
			DataDir:     tmpDir,
		}

		mgr, err := NewManager(cfg)
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if mgr == nil {
			t.Fatal("NewManager() returned nil")
		}
	})

	t.Run("with custom HTTPClient", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := Config{
			AppName:     "testapp",
			RegistryURL: "https://example.com",
			DataDir:     tmpDir,
		}

		customClient := &http.Client{}
		mgr, err := NewManager(cfg, WithHTTPClient(customClient))
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if mgr == nil {
			t.Fatal("NewManager() returned nil")
		}

		// Verify the custom client was used
		m := mgr.(*manager)
		if m.httpClient != customClient {
			t.Error("custom HTTP client was not set")
		}
	})

	t.Run("with custom Logger", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := Config{
			AppName:     "testapp",
			RegistryURL: "https://example.com",
			DataDir:     tmpDir,
		}

		logger := &testLogger{}
		mgr, err := NewManager(cfg, WithLogger(logger))
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}
		if mgr == nil {
			t.Fatal("NewManager() returned nil")
		}

		// Verify the logger was set
		m := mgr.(*manager)
		if m.logger != logger {
			t.Error("custom logger was not set")
		}
	})
}

func TestManagerImplementsInterface(t *testing.T) {
	// This is a compile-time check that *manager implements Manager
	var _ Manager = (*manager)(nil)

	// Also verify at runtime with a real instance
	tmpDir := t.TempDir()
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
		DataDir:     tmpDir,
	}

	mgr, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Type assertion should succeed
	if _, ok := mgr.(Manager); !ok {
		t.Error("returned value does not implement Manager interface")
	}
}

func TestNewManagerCreatesStorageDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
		DataDir:     tmpDir,
	}

	_, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// The storage directory should have been created
	// (newStorage creates it in its constructor)
}
