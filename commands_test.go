package models

import (
	"bytes"
	"testing"
)

func TestNewCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)

	t.Run("root command exists", func(t *testing.T) {
		if cmd == nil {
			t.Fatal("NewCommand returned nil")
		}
		if cmd.Use != "models" {
			t.Errorf("Use = %q, want %q", cmd.Use, "models")
		}
	})

	t.Run("has global flags", func(t *testing.T) {
		flags := []string{"json", "quiet", "verbose"}
		for _, name := range flags {
			if cmd.PersistentFlags().Lookup(name) == nil {
				t.Errorf("missing global flag: %s", name)
			}
		}
	})

	t.Run("has subcommands", func(t *testing.T) {
		subcommands := []string{"list", "pull", "remove", "info", "path", "update"}
		for _, name := range subcommands {
			found := false
			for _, sub := range cmd.Commands() {
				if sub.Name() == name {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing subcommand: %s", name)
			}
		}
	})
}

func TestListCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	listCmd, _, err := cmd.Find([]string{"list"})
	if err != nil {
		t.Fatalf("finding list command: %v", err)
	}

	t.Run("has remote flag", func(t *testing.T) {
		if listCmd.Flags().Lookup("remote") == nil {
			t.Error("missing --remote flag")
		}
	})
}

func TestPullCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	pullCmd, _, err := cmd.Find([]string{"pull"})
	if err != nil {
		t.Fatalf("finding pull command: %v", err)
	}

	t.Run("has force flag", func(t *testing.T) {
		if pullCmd.Flags().Lookup("force") == nil {
			t.Error("missing --force flag")
		}
	})

	t.Run("requires two arguments", func(t *testing.T) {
		// Verify args validation is set up
		if pullCmd.Args == nil {
			t.Error("Args validator not set")
		}
	})
}

func TestRemoveCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	removeCmd, _, err := cmd.Find([]string{"remove"})
	if err != nil {
		t.Fatalf("finding remove command: %v", err)
	}

	t.Run("has all flag", func(t *testing.T) {
		if removeCmd.Flags().Lookup("all") == nil {
			t.Error("missing --all flag")
		}
	})
}

func TestInfoCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	infoCmd, _, err := cmd.Find([]string{"info"})
	if err != nil {
		t.Fatalf("finding info command: %v", err)
	}

	t.Run("has remote flag", func(t *testing.T) {
		if infoCmd.Flags().Lookup("remote") == nil {
			t.Error("missing --remote flag")
		}
	})
}

func TestPathCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	pathCmd, _, err := cmd.Find([]string{"path"})
	if err != nil {
		t.Fatalf("finding path command: %v", err)
	}

	t.Run("exists", func(t *testing.T) {
		if pathCmd == nil {
			t.Error("path command not found")
		}
	})
}

func TestUpdateCommand(t *testing.T) {
	cfg := Config{
		AppName:     "testapp",
		RegistryURL: "https://example.com",
	}

	cmd := NewCommand(cfg)
	updateCmd, _, err := cmd.Find([]string{"update"})
	if err != nil {
		t.Fatalf("finding update command: %v", err)
	}

	t.Run("has apply flag", func(t *testing.T) {
		if updateCmd.Flags().Lookup("apply") == nil {
			t.Error("missing --apply flag")
		}
	})
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1572864, "1.50 MB"},
		{1073741824, "1.00 GB"},
		{1610612736, "1.50 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatSize(tt.bytes)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestOutputInstalledModels(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		var buf bytes.Buffer
		err := outputInstalledModels(&buf, []InstalledModel{}, false)
		if err != nil {
			t.Fatalf("outputInstalledModels() error = %v", err)
		}
		if buf.String() != "No models installed\n" {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})

	t.Run("json output empty", func(t *testing.T) {
		var buf bytes.Buffer
		err := outputInstalledModels(&buf, []InstalledModel{}, true)
		if err != nil {
			t.Fatalf("outputInstalledModels() error = %v", err)
		}
		if buf.String() != "[]\n" {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})
}

func TestOutputRemoteModels(t *testing.T) {
	t.Run("empty list", func(t *testing.T) {
		var buf bytes.Buffer
		err := outputRemoteModels(&buf, []RemoteModel{}, false)
		if err != nil {
			t.Fatalf("outputRemoteModels() error = %v", err)
		}
		if buf.String() != "No models found in registry\n" {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})

	t.Run("json output empty", func(t *testing.T) {
		var buf bytes.Buffer
		err := outputRemoteModels(&buf, []RemoteModel{}, true)
		if err != nil {
			t.Fatalf("outputRemoteModels() error = %v", err)
		}
		if buf.String() != "[]\n" {
			t.Errorf("unexpected output: %q", buf.String())
		}
	})
}
