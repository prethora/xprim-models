package models

import (
	"errors"
	"testing"
)

func TestModelRefString(t *testing.T) {
	tests := []struct {
		name string
		ref  ModelRef
		want string
	}{
		{
			name: "with version",
			ref:  ModelRef{Group: "fast-whisper", Model: "tiny", Version: "fp16"},
			want: "fast-whisper/tiny fp16",
		},
		{
			name: "without version",
			ref:  ModelRef{Group: "fast-whisper", Model: "tiny", Version: ""},
			want: "fast-whisper/tiny",
		},
		{
			name: "different group and model",
			ref:  ModelRef{Group: "llama", Model: "7b", Version: "q4"},
			want: "llama/7b q4",
		},
		{
			name: "empty version explicit",
			ref:  ModelRef{Group: "group", Model: "model"},
			want: "group/model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ref.String()
			if got != tt.want {
				t.Errorf("ModelRef.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseModelRef(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantRef   ModelRef
		wantErr   error
		wantErrIs bool // if true, use errors.Is instead of exact match
	}{
		{
			name:    "valid with version",
			input:   "fast-whisper/tiny fp16",
			wantRef: ModelRef{Group: "fast-whisper", Model: "tiny", Version: "fp16"},
		},
		{
			name:    "valid without version",
			input:   "fast-whisper/tiny",
			wantRef: ModelRef{Group: "fast-whisper", Model: "tiny", Version: ""},
		},
		{
			name:    "valid with different version",
			input:   "group/model int8",
			wantRef: ModelRef{Group: "group", Model: "model", Version: "int8"},
		},
		{
			name:    "valid with version containing spaces",
			input:   "group/model version with spaces",
			wantRef: ModelRef{Group: "group", Model: "model", Version: "version with spaces"},
		},
		{
			name:      "invalid empty string",
			input:     "",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid no slash",
			input:     "noSlash",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid empty group",
			input:     "/model",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid empty model",
			input:     "group/",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid multiple slashes",
			input:     "a/b/c",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid multiple slashes with version",
			input:     "a/b/c version",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
		{
			name:      "invalid just slash",
			input:     "/",
			wantErr:   ErrInvalidRef,
			wantErrIs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseModelRef(tt.input)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("ParseModelRef(%q) error = nil, want error", tt.input)
					return
				}
				if tt.wantErrIs {
					if !errors.Is(err, tt.wantErr) {
						t.Errorf("ParseModelRef(%q) error = %v, want %v", tt.input, err, tt.wantErr)
					}
				} else if err != tt.wantErr {
					t.Errorf("ParseModelRef(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseModelRef(%q) unexpected error = %v", tt.input, err)
				return
			}

			if got != tt.wantRef {
				t.Errorf("ParseModelRef(%q) = %+v, want %+v", tt.input, got, tt.wantRef)
			}
		})
	}
}

func TestParseModelRefRoundTrip(t *testing.T) {
	// Test that parsing the String() output gives back the same ref
	refs := []ModelRef{
		{Group: "fast-whisper", Model: "tiny", Version: "fp16"},
		{Group: "llama", Model: "7b", Version: "q4"},
		{Group: "group", Model: "model", Version: ""},
	}

	for _, ref := range refs {
		t.Run(ref.String(), func(t *testing.T) {
			s := ref.String()
			parsed, err := ParseModelRef(s)
			if err != nil {
				t.Errorf("ParseModelRef(%q) unexpected error = %v", s, err)
				return
			}
			if parsed != ref {
				t.Errorf("Round trip failed: %+v -> %q -> %+v", ref, s, parsed)
			}
		})
	}
}
