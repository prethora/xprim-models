package models

import (
	"net/http"
	"testing"
)

func TestWithConcurrency(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{
			name:  "default value",
			input: -1, // will use default
			want:  DefaultConcurrency,
		},
		{
			name:  "zero clamped to 1",
			input: 0,
			want:  1,
		},
		{
			name:  "negative clamped to 1",
			input: -5,
			want:  1,
		},
		{
			name:  "above max clamped to MaxConcurrency",
			input: 100,
			want:  MaxConcurrency,
		},
		{
			name:  "exactly MaxConcurrency",
			input: MaxConcurrency,
			want:  MaxConcurrency,
		},
		{
			name:  "valid value preserved",
			input: 8,
			want:  8,
		},
		{
			name:  "minimum valid value",
			input: 1,
			want:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newPullConfig()

			// For the "default value" test, don't apply any option
			if tt.name != "default value" {
				WithConcurrency(tt.input)(cfg)
			}

			if cfg.concurrency != tt.want {
				t.Errorf("concurrency = %d, want %d", cfg.concurrency, tt.want)
			}
		})
	}
}

func TestWithForce(t *testing.T) {
	t.Run("default is false", func(t *testing.T) {
		cfg := newPullConfig()
		if cfg.force {
			t.Error("default force should be false")
		}
	})

	t.Run("WithForce sets to true", func(t *testing.T) {
		cfg := newPullConfig()
		WithForce()(cfg)
		if !cfg.force {
			t.Error("force should be true after WithForce()")
		}
	})
}

func TestWithProgress(t *testing.T) {
	t.Run("default progressFn is nil", func(t *testing.T) {
		cfg := newPullConfig()
		if cfg.progressFn != nil {
			t.Error("default progressFn should be nil")
		}
	})

	t.Run("WithProgress assigns callback", func(t *testing.T) {
		cfg := newPullConfig()
		called := false
		fn := func(p PullProgress) {
			called = true
		}

		WithProgress(fn)(cfg)

		if cfg.progressFn == nil {
			t.Error("progressFn should not be nil after WithProgress()")
		}

		// Verify callback can be invoked
		cfg.progressFn(PullProgress{Phase: "test"})
		if !called {
			t.Error("progressFn was not invoked")
		}
	})
}

func TestManagerOptions(t *testing.T) {
	t.Run("default httpClient is http.DefaultClient", func(t *testing.T) {
		cfg := newManagerConfig()
		if cfg.httpClient != http.DefaultClient {
			t.Error("default httpClient should be http.DefaultClient")
		}
	})

	t.Run("default logger is nil", func(t *testing.T) {
		cfg := newManagerConfig()
		if cfg.logger != nil {
			t.Error("default logger should be nil")
		}
	})

	t.Run("WithHTTPClient sets custom client", func(t *testing.T) {
		cfg := newManagerConfig()
		customClient := &http.Client{}

		WithHTTPClient(customClient)(cfg)

		if cfg.httpClient != customClient {
			t.Error("httpClient should be the custom client")
		}
	})

	t.Run("WithLogger sets logger", func(t *testing.T) {
		cfg := newManagerConfig()
		logger := &testLogger{}

		WithLogger(logger)(cfg)

		if cfg.logger != logger {
			t.Error("logger should be set")
		}
	})
}

// testLogger is a simple Logger implementation for testing.
type testLogger struct {
	messages []string
}

func (l *testLogger) Debug(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, "DEBUG: "+msg)
}

func (l *testLogger) Info(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, "INFO: "+msg)
}

func (l *testLogger) Warn(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, "WARN: "+msg)
}

func (l *testLogger) Error(msg string, keysAndValues ...any) {
	l.messages = append(l.messages, "ERROR: "+msg)
}

func TestConstants(t *testing.T) {
	// Verify constants have expected values per architecture doc
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"DefaultConcurrency", DefaultConcurrency, 4},
		{"MaxConcurrency", MaxConcurrency, 16},
		{"MaxRetries", MaxRetries, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
