package models

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModelsIndex(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models.json" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"fast-whisper": {
					"tiny": {
						"fp16": {"hash": "abc123", "metadata": {"framework": "onnx"}},
						"int8": {"hash": "def456", "metadata": {}}
					}
				}
			}`))
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		index, err := client.fetchModelsIndex(context.Background())
		if err != nil {
			t.Fatalf("fetchModelsIndex() error = %v", err)
		}

		// Verify structure
		groupModels, ok := index["fast-whisper"]
		if !ok {
			t.Fatal("expected fast-whisper group")
		}

		modelVersions, ok := groupModels["tiny"]
		if !ok {
			t.Fatal("expected tiny model")
		}

		if len(modelVersions) != 2 {
			t.Errorf("expected 2 versions, got %d", len(modelVersions))
		}

		fp16 := modelVersions["fp16"]
		if fp16.Hash != "abc123" {
			t.Errorf("fp16 hash = %q, want %q", fp16.Hash, "abc123")
		}

		int8 := modelVersions["int8"]
		if int8.Hash != "def456" {
			t.Errorf("int8 hash = %q, want %q", int8.Hash, "def456")
		}

		if fp16.Metadata["framework"] != "onnx" {
			t.Errorf("fp16 metadata framework = %v, want %q", fp16.Metadata["framework"], "onnx")
		}
	})

	t.Run("network error", func(t *testing.T) {
		// Create a server and immediately close it to simulate network error
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchModelsIndex(context.Background())

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNetworkError) {
			t.Errorf("expected ErrNetworkError, got %v", err)
		}
	})

	t.Run("non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchModelsIndex(context.Background())

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrRegistryError) {
			t.Errorf("expected ErrRegistryError, got %v", err)
		}
	})

	t.Run("malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{invalid json`))
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchModelsIndex(context.Background())

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrRegistryError) {
			t.Errorf("expected ErrRegistryError, got %v", err)
		}
	})
}

func TestFetchManifest(t *testing.T) {
	manifestHash := "abc123def456"

	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/abc123def456" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"total_size": 1048576,
				"chunk_size": 262144,
				"chunks": ["chunk1", "chunk2", "chunk3", "chunk4"],
				"files": [
					{"path": "model.bin", "size": 1000000},
					{"path": "config.json", "size": 48576}
				]
			}`))
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		mf, err := client.fetchManifest(context.Background(), manifestHash)
		if err != nil {
			t.Fatalf("fetchManifest() error = %v", err)
		}

		if mf.TotalSize != 1048576 {
			t.Errorf("TotalSize = %d, want %d", mf.TotalSize, 1048576)
		}
		if mf.ChunkSize != 262144 {
			t.Errorf("ChunkSize = %d, want %d", mf.ChunkSize, 262144)
		}
		if len(mf.Chunks) != 4 {
			t.Errorf("len(Chunks) = %d, want %d", len(mf.Chunks), 4)
		}
		if len(mf.Files) != 2 {
			t.Errorf("len(Files) = %d, want %d", len(mf.Files), 2)
		}
		if mf.Files[0].Path != "model.bin" {
			t.Errorf("Files[0].Path = %q, want %q", mf.Files[0].Path, "model.bin")
		}
		if mf.Files[0].Size != 1000000 {
			t.Errorf("Files[0].Size = %d, want %d", mf.Files[0].Size, 1000000)
		}
	})

	t.Run("404 returns ErrModelNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchManifest(context.Background(), manifestHash)

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrModelNotFound) {
			t.Errorf("expected ErrModelNotFound, got %v", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchManifest(context.Background(), manifestHash)

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNetworkError) {
			t.Errorf("expected ErrNetworkError, got %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchManifest(context.Background(), manifestHash)

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrRegistryError) {
			t.Errorf("expected ErrRegistryError, got %v", err)
		}
	})
}

func TestFetchChunk(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedData := []byte("chunk data content here")

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/chunkhash" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Write(expectedData)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		data, err := client.fetchChunk(context.Background(), "chunkhash")
		if err != nil {
			t.Fatalf("fetchChunk() error = %v", err)
		}

		if string(data) != string(expectedData) {
			t.Errorf("data = %q, want %q", string(data), string(expectedData))
		}
	})

	t.Run("404 returns ErrModelNotFound", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchChunk(context.Background(), "nonexistent")

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrModelNotFound) {
			t.Errorf("expected ErrModelNotFound, got %v", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchChunk(context.Background(), "chunkhash")

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrNetworkError) {
			t.Errorf("expected ErrNetworkError, got %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := newRegistryClient(server.URL, server.Client(), nil)
		_, err := client.fetchChunk(context.Background(), "chunkhash")

		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrRegistryError) {
			t.Errorf("expected ErrRegistryError, got %v", err)
		}
	})
}

func TestRegistryErrorWrapping(t *testing.T) {
	// Verify that errors.Is works correctly with wrapped errors
	tests := []struct {
		name     string
		setup    func(*httptest.Server)
		method   string
		expected error
	}{
		{
			name: "fetchModelsIndex network error",
			setup: func(s *httptest.Server) {
				s.Close()
			},
			method:   "index",
			expected: ErrNetworkError,
		},
		{
			name: "fetchManifest 404",
			setup: func(s *httptest.Server) {
				// Server will return 404
			},
			method:   "manifest404",
			expected: ErrModelNotFound,
		},
		{
			name: "fetchChunk 404",
			setup: func(s *httptest.Server) {
				// Server will return 404
			},
			method:   "chunk404",
			expected: ErrModelNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			}))

			if tt.method == "index" {
				// Close server to simulate network error
				server.Close()
			} else {
				defer server.Close()
			}

			client := newRegistryClient(server.URL, server.Client(), nil)
			ctx := context.Background()

			var err error
			switch tt.method {
			case "index":
				_, err = client.fetchModelsIndex(ctx)
			case "manifest404":
				_, err = client.fetchManifest(ctx, "nonexistent")
			case "chunk404":
				_, err = client.fetchChunk(ctx, "nonexistent")
			}

			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tt.expected) {
				t.Errorf("errors.Is(%v, %v) = false, want true", err, tt.expected)
			}
		})
	}
}

func TestRegistryClientWithContext(t *testing.T) {
	// Test that context cancellation is respected
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler shouldn't be reached with cancelled context
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newRegistryClient(server.URL, server.Client(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.fetchModelsIndex(ctx)
	if err == nil {
		t.Error("expected error with cancelled context")
	}
}
