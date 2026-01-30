package models

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorMessages(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantMsg string
	}{
		{
			name:    "ErrModelNotFound",
			err:     ErrModelNotFound,
			wantMsg: "models: model not found in registry",
		},
		{
			name:    "ErrVersionNotFound",
			err:     ErrVersionNotFound,
			wantMsg: "models: version not found",
		},
		{
			name:    "ErrNotInstalled",
			err:     ErrNotInstalled,
			wantMsg: "models: model not installed",
		},
		{
			name:    "ErrAlreadyInstalled",
			err:     ErrAlreadyInstalled,
			wantMsg: "models: model already installed",
		},
		{
			name:    "ErrHashMismatch",
			err:     ErrHashMismatch,
			wantMsg: "models: hash verification failed",
		},
		{
			name:    "ErrNetworkError",
			err:     ErrNetworkError,
			wantMsg: "models: network error",
		},
		{
			name:    "ErrStorageError",
			err:     ErrStorageError,
			wantMsg: "models: storage error",
		},
		{
			name:    "ErrInvalidRef",
			err:     ErrInvalidRef,
			wantMsg: "models: invalid model reference",
		},
		{
			name:    "ErrRegistryError",
			err:     ErrRegistryError,
			wantMsg: "models: invalid registry response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()

			// Verify message starts with "models: " prefix
			if !strings.HasPrefix(got, "models: ") {
				t.Errorf("%s: message %q does not have 'models: ' prefix", tt.name, got)
			}

			// Verify exact message content
			if got != tt.wantMsg {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.wantMsg)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrModelNotFound", ErrModelNotFound},
		{"ErrVersionNotFound", ErrVersionNotFound},
		{"ErrNotInstalled", ErrNotInstalled},
		{"ErrAlreadyInstalled", ErrAlreadyInstalled},
		{"ErrHashMismatch", ErrHashMismatch},
		{"ErrNetworkError", ErrNetworkError},
		{"ErrStorageError", ErrStorageError},
		{"ErrInvalidRef", ErrInvalidRef},
		{"ErrRegistryError", ErrRegistryError},
	}

	for _, tt := range sentinels {
		t.Run(tt.name, func(t *testing.T) {
			// Wrap the error with additional context
			wrapped := fmt.Errorf("operation failed: %w", tt.err)

			// Verify errors.Is() still matches the sentinel
			if !errors.Is(wrapped, tt.err) {
				t.Errorf("errors.Is(wrapped, %s) = false, want true", tt.name)
			}

			// Double-wrap to ensure chain works
			doubleWrapped := fmt.Errorf("outer context: %w", wrapped)
			if !errors.Is(doubleWrapped, tt.err) {
				t.Errorf("errors.Is(doubleWrapped, %s) = false, want true", tt.name)
			}
		})
	}
}
