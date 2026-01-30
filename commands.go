package models

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

// NewCommand creates a Cobra command tree for model management.
// The returned command should be added to a parent CLI's root command.
//
// Commands provided:
//   - models list [--remote]
//   - models pull <group/model> <version> [--force]
//   - models remove <group/model> <version> [--all]
//   - models info <group/model> <version> [--remote]
//   - models path <group/model> <version>
//   - models update [<group/model>] [--apply]
//
// Global flags: --json, --quiet, --verbose
func NewCommand(cfg Config, opts ...ManagerOption) *cobra.Command {
	var (
		jsonOutput bool
		quiet      bool
		verbose    bool
	)

	// Manager will be created in PersistentPreRunE
	var mgr Manager

	cmd := &cobra.Command{
		Use:   "models",
		Short: "Manage ML models",
		Long:  "Download, install, and manage ML models from a content-addressed registry.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip manager creation for help commands
			if cmd.Name() == "help" || cmd.Name() == "completion" {
				return nil
			}

			var err error
			mgr, err = NewManager(cfg, opts...)
			if err != nil {
				return fmt.Errorf("failed to initialize manager: %w", err)
			}
			return nil
		},
		SilenceUsage: true,
	}

	cmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	cmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "Suppress non-essential output")
	cmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Verbose output")

	// Add subcommands
	cmd.AddCommand(listCmd(&mgr, &jsonOutput, &quiet))
	cmd.AddCommand(pullCmd(&mgr, &quiet, &verbose))
	cmd.AddCommand(removeCmd(&mgr, &quiet))
	cmd.AddCommand(infoCmd(&mgr, &jsonOutput))
	cmd.AddCommand(pathCmd(&mgr))
	cmd.AddCommand(updateCmd(&mgr, &jsonOutput, &quiet))
	cmd.AddCommand(pruneCmd(&mgr, &quiet))

	return cmd
}

func listCmd(mgr *Manager, jsonOutput, quiet *bool) *cobra.Command {
	var remote bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List models",
		Long:  "List installed models, or remote models with --remote flag.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if remote {
				models, err := (*mgr).ListRemote(ctx)
				if err != nil {
					return err
				}
				return outputRemoteModels(cmd.OutOrStdout(), models, *jsonOutput)
			}

			models, err := (*mgr).ListInstalled(ctx)
			if err != nil {
				return err
			}
			return outputInstalledModels(cmd.OutOrStdout(), models, *jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&remote, "remote", false, "List remote models from registry")
	return cmd
}

func pullCmd(mgr *Manager, quiet, verbose *bool) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "pull <group/model> <version>",
		Short: "Download and install a model",
		Long:  "Download a model from the registry and install it locally.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			ref, err := ParseModelRef(args[0] + " " + args[1])
			if err != nil {
				return err
			}

			var opts []PullOption
			if force {
				opts = append(opts, WithForce())
			}

			// Set up progress reporting
			if !*quiet {
				var progressMu sync.Mutex
				var ticker *time.Ticker
				var tickerDone chan struct{}
				var currentBytes int64
				var totalBytes int64
				var bytesDownloaded int64  // Completed network bytes (for speed calc)
				var bytesInProgress int64  // Currently downloading bytes (for smooth speed)
				var startTime time.Time
				var progressStarted bool

				opts = append(opts, WithProgress(func(p PullProgress) {
					switch p.Phase {
					case "manifest":
						fmt.Fprintf(cmd.OutOrStdout(), "Fetching manifest...\n")
					case "chunks":
						progressMu.Lock()
						if !progressStarted && p.BytesTotal > 0 {
							startTime = time.Now()
							totalBytes = p.BytesTotal
							progressStarted = true
							// Hide cursor and render initial progress
							fmt.Fprint(cmd.OutOrStdout(), "\x1b[?25l")
							renderProgress(cmd.OutOrStdout(), p.BytesCompleted, totalBytes, 0, 0, startTime)
							// Start ticker for periodic updates
							ticker = time.NewTicker(time.Second)
							tickerDone = make(chan struct{})
							go func() {
								for {
									select {
									case <-ticker.C:
										progressMu.Lock()
										if progressStarted {
											renderProgress(cmd.OutOrStdout(), currentBytes, totalBytes, bytesDownloaded, bytesInProgress, startTime)
										}
										progressMu.Unlock()
									case <-tickerDone:
										return
									}
								}
							}()
						}
						currentBytes = p.BytesCompleted
						bytesDownloaded = p.BytesDownloaded
						bytesInProgress = p.BytesInProgress
						if progressStarted {
							renderProgress(cmd.OutOrStdout(), currentBytes, totalBytes, bytesDownloaded, bytesInProgress, startTime)
						}
						progressMu.Unlock()
					case "extracting":
						progressMu.Lock()
						if ticker != nil {
							ticker.Stop()
							close(tickerDone)
							ticker = nil
						}
						if progressStarted {
							// Final render at 100%, show cursor, new line
							renderProgress(cmd.OutOrStdout(), totalBytes, totalBytes, bytesDownloaded, 0, startTime)
							fmt.Fprint(cmd.OutOrStdout(), "\x1b[?25h\n") // Show cursor and new line
							progressStarted = false
						}
						progressMu.Unlock()
						if *verbose && p.CurrentFile != "" {
							fmt.Fprintf(cmd.OutOrStdout(), "Extracting: %s\n", p.CurrentFile)
						}
					}
				}))
			}

			err = (*mgr).Pull(ctx, ref, opts...)
			if err != nil {
				if errors.Is(err, ErrAlreadyInstalled) {
					if !*quiet {
						fmt.Fprintf(cmd.OutOrStdout(), "Model %s is already installed (use --force to re-download)\n", ref)
					}
					return nil
				}
				return err
			}

			if !*quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "\nSuccessfully installed %s\n", ref)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-download even if already installed")
	return cmd
}

func removeCmd(mgr *Manager, quiet *bool) *cobra.Command {
	var (
		all bool
		yes bool
	)

	cmd := &cobra.Command{
		Use:   "remove <group/model> [version]",
		Short: "Remove an installed model",
		Long:  "Remove an installed model. Use --all to remove all versions.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			if all {
				// Parse group/model only
				parts := strings.Split(args[0], "/")
				if len(parts) != 2 {
					return fmt.Errorf("invalid format: expected group/model")
				}
				group, model := parts[0], parts[1]

				// Confirmation prompt
				if !yes {
					fmt.Fprintf(cmd.OutOrStdout(), "Remove all versions of %s/%s? [y/N]: ", group, model)
					if !confirmPrompt(cmd.InOrStdin()) {
						fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
						return nil
					}
				}

				err := (*mgr).RemoveAll(ctx, group, model)
				if err != nil {
					return err
				}

				if !*quiet {
					fmt.Fprintf(cmd.OutOrStdout(), "Removed all versions of %s/%s\n", group, model)
				}
				return nil
			}

			// Need version for single removal
			if len(args) < 2 {
				return fmt.Errorf("version required (or use --all to remove all versions)")
			}

			ref, err := ParseModelRef(args[0] + " " + args[1])
			if err != nil {
				return err
			}

			// Confirmation prompt
			if !yes {
				fmt.Fprintf(cmd.OutOrStdout(), "Remove %s? [y/N]: ", ref)
				if !confirmPrompt(cmd.InOrStdin()) {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			err = (*mgr).Remove(ctx, ref)
			if err != nil {
				return err
			}

			if !*quiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", ref)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Remove all versions of the model")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func infoCmd(mgr *Manager, jsonOutput *bool) *cobra.Command {
	var remote bool

	cmd := &cobra.Command{
		Use:   "info <group/model> <version>",
		Short: "Show model information",
		Long:  "Show detailed information about a model.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			ref, err := ParseModelRef(args[0] + " " + args[1])
			if err != nil {
				return err
			}

			if remote {
				detail, err := (*mgr).GetRemoteDetail(ctx, ref)
				if err != nil {
					return err
				}
				return outputRemoteDetail(cmd.OutOrStdout(), detail, *jsonOutput)
			}

			installed, err := (*mgr).GetInstalled(ctx, ref)
			if err != nil {
				return err
			}
			return outputInstalledDetail(cmd.OutOrStdout(), installed, *jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&remote, "remote", false, "Show remote model info from registry")
	return cmd
}

func pathCmd(mgr *Manager) *cobra.Command {
	return &cobra.Command{
		Use:   "path <group/model> <version>",
		Short: "Print path to installed model",
		Long:  "Print the filesystem path to an installed model's directory.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			ref, err := ParseModelRef(args[0] + " " + args[1])
			if err != nil {
				return err
			}

			path, err := (*mgr).Path(ctx, ref)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}
}

func updateCmd(mgr *Manager, jsonOutput, quiet *bool) *cobra.Command {
	var apply bool

	cmd := &cobra.Command{
		Use:   "update [group/model] [version]",
		Short: "Check for model updates",
		Long:  "Check if updates are available for installed models. Use --apply to download updates.",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			type updateInfo struct {
				Ref           ModelRef `json:"ref"`
				InstalledHash string   `json:"installed_hash"`
				RemoteHash    string   `json:"remote_hash"`
				UpdateAvail   bool     `json:"update_available"`
			}

			var updates []updateInfo

			if len(args) >= 2 {
				// Check specific model
				ref, err := ParseModelRef(args[0] + " " + args[1])
				if err != nil {
					return err
				}

				installed, remote, err := (*mgr).CheckUpdate(ctx, ref)
				if err != nil {
					return err
				}

				updates = append(updates, updateInfo{
					Ref:           ref,
					InstalledHash: installed,
					RemoteHash:    remote,
					UpdateAvail:   installed != remote,
				})
			} else {
				// Check all installed models
				models, err := (*mgr).ListInstalled(ctx)
				if err != nil {
					return err
				}

				for _, m := range models {
					installed, remote, err := (*mgr).CheckUpdate(ctx, m.Ref)
					if err != nil {
						// Skip models that fail to check
						continue
					}
					updates = append(updates, updateInfo{
						Ref:           m.Ref,
						InstalledHash: installed,
						RemoteHash:    remote,
						UpdateAvail:   installed != remote,
					})
				}
			}

			// Output results
			if *jsonOutput {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(updates)
			}

			hasUpdates := false
			for _, u := range updates {
				if u.UpdateAvail {
					hasUpdates = true
					fmt.Fprintf(cmd.OutOrStdout(), "%s: update available\n", u.Ref)

					if apply {
						if !*quiet {
							fmt.Fprintf(cmd.OutOrStdout(), "  Updating %s...\n", u.Ref)
						}
						if err := (*mgr).Pull(ctx, u.Ref, WithForce()); err != nil {
							fmt.Fprintf(cmd.OutOrStdout(), "  Error: %v\n", err)
						} else if !*quiet {
							fmt.Fprintf(cmd.OutOrStdout(), "  Updated successfully\n")
						}
					}
				} else if !*quiet {
					fmt.Fprintf(cmd.OutOrStdout(), "%s: up to date\n", u.Ref)
				}
			}

			if !hasUpdates && !*quiet {
				fmt.Fprintln(cmd.OutOrStdout(), "All models are up to date")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&apply, "apply", false, "Apply available updates")
	return cmd
}

func pruneCmd(mgr *Manager, quiet *bool) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Clear the chunk cache",
		Long:  "Remove all cached chunks from incomplete downloads. This frees disk space but may require re-downloading chunks if a download is resumed.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Confirmation prompt
			if !yes {
				fmt.Fprint(cmd.OutOrStdout(), "Clear the chunk cache? [y/N]: ")
				if !confirmPrompt(cmd.InOrStdin()) {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			err := (*mgr).PruneCache(ctx)
			if err != nil {
				return err
			}

			if !*quiet {
				fmt.Fprintln(cmd.OutOrStdout(), "Chunk cache cleared.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// confirmPrompt reads from stdin and returns true only if the user types 'y' or 'Y'.
// Returns false for empty input or any other response (default is no).
func confirmPrompt(r io.Reader) bool {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		response := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return response == "y" || response == "yes"
	}
	return false
}

// Output helpers

func outputInstalledModels(w io.Writer, models []InstalledModel, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(models)
	}

	if len(models) == 0 {
		fmt.Fprintln(w, "No models installed")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tVERSION\tSIZE\tINSTALLED")
	for _, m := range models {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\n",
			m.Ref.Group, m.Ref.Model,
			m.Ref.Version,
			formatSize(m.TotalSize),
			m.InstalledAt.Format("2006-01-02 15:04"),
		)
	}
	return tw.Flush()
}

func outputRemoteModels(w io.Writer, models []RemoteModel, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(models)
	}

	if len(models) == 0 {
		fmt.Fprintln(w, "No models found in registry")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "MODEL\tVERSION\tHASH")
	for _, m := range models {
		hash := m.ManifestHash
		if len(hash) > 12 {
			hash = hash[:12] + "..."
		}
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\n",
			m.Ref.Group, m.Ref.Model,
			m.Ref.Version,
			hash,
		)
	}
	return tw.Flush()
}

func outputInstalledDetail(w io.Writer, m InstalledModel, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}

	fmt.Fprintf(w, "Model:        %s/%s\n", m.Ref.Group, m.Ref.Model)
	fmt.Fprintf(w, "Version:      %s\n", m.Ref.Version)
	fmt.Fprintf(w, "Size:         %s\n", formatSize(m.TotalSize))
	fmt.Fprintf(w, "Files:        %d\n", m.FileCount)
	fmt.Fprintf(w, "Installed:    %s\n", m.InstalledAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(w, "Path:         %s\n", m.Path)
	fmt.Fprintf(w, "Manifest:     %s\n", m.ManifestHash[:16]+"...")
	return nil
}

func outputRemoteDetail(w io.Writer, m RemoteModelDetail, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(m)
	}

	fmt.Fprintf(w, "Model:        %s/%s\n", m.Ref.Group, m.Ref.Model)
	fmt.Fprintf(w, "Version:      %s\n", m.Ref.Version)
	fmt.Fprintf(w, "Size:         %s\n", formatSize(m.TotalSize))
	fmt.Fprintf(w, "Chunks:       %d (chunk size: %s)\n", m.ChunkCount, formatSize(m.ChunkSize))
	fmt.Fprintf(w, "Files:        %d\n", len(m.Files))
	fmt.Fprintf(w, "Manifest:     %s\n", m.ManifestHash[:16]+"...")

	if len(m.Files) > 0 {
		fmt.Fprintln(w, "\nFiles:")
		for _, f := range m.Files {
			fmt.Fprintf(w, "  %s (%s)\n", f.Path, formatSize(f.Size))
		}
	}
	return nil
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// renderProgress renders the progress bar to the writer.
// Format: Downloading [============>                 ] 45% (5.2 MB/s, elapsed: 30s, remaining: 2m 15s)
// bytesDownloaded is completed network bytes, bytesInProgress is currently downloading bytes.
// Speed is calculated from bytesDownloaded + bytesInProgress for smooth display.
func renderProgress(w io.Writer, current, total, bytesDownloaded, bytesInProgress int64, startTime time.Time) {
	elapsed := time.Since(startTime)

	// Calculate percentage
	var pct float64
	if total > 0 {
		pct = float64(current) / float64(total) * 100
	}

	// Calculate speed (bytes per second) - include both completed and in-progress bytes
	// This gives smooth speed updates even while chunks are downloading
	var speed float64
	totalNetworkBytes := bytesDownloaded + bytesInProgress
	if elapsed.Seconds() > 0 && totalNetworkBytes > 0 {
		speed = float64(totalNetworkBytes) / elapsed.Seconds()
	}

	// Calculate remaining time based on current speed
	// Remaining bytes = total - current (includes both cached and downloaded)
	var remaining time.Duration
	if speed > 0 && current < total {
		remaining = time.Duration(float64(total-current)/speed) * time.Second
	}

	// Build progress bar
	const barWidth = 30
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	var bar string
	if filled >= barWidth {
		bar = strings.Repeat("=", barWidth)
	} else if filled > 0 {
		bar = strings.Repeat("=", filled) + ">" + strings.Repeat(" ", barWidth-filled-1)
	} else {
		bar = ">" + strings.Repeat(" ", barWidth-1)
	}

	// Format and print (using \r to overwrite, \x1b[K to clear to end of line)
	fmt.Fprintf(w, "\r\x1b[KDownloading [%s] %.0f%% (%s, elapsed: %s, remaining: %s)",
		bar, pct, formatSpeed(speed), formatDuration(elapsed), formatDuration(remaining))
}

// formatSpeed formats bytes per second as KB/s or MB/s.
func formatSpeed(bytesPerSec float64) string {
	const (
		KB = 1024
		MB = KB * 1024
	)

	if bytesPerSec >= MB {
		return fmt.Sprintf("%.1f MB/s", bytesPerSec/MB)
	}
	if bytesPerSec >= KB {
		return fmt.Sprintf("%.1f KB/s", bytesPerSec/KB)
	}
	return fmt.Sprintf("%.0f B/s", bytesPerSec)
}

// formatDuration formats a duration as human-readable text (e.g., "5s", "2m 30s", "1h 5m").
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	d = d.Round(time.Second)

	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	secs := int(d.Seconds()) % 60

	if hours > 0 {
		if mins > 0 {
			return fmt.Sprintf("%dh %dm", hours, mins)
		}
		return fmt.Sprintf("%dh", hours)
	}
	if mins > 0 {
		if secs > 0 {
			return fmt.Sprintf("%dm %ds", mins, secs)
		}
		return fmt.Sprintf("%dm", mins)
	}
	return fmt.Sprintf("%ds", secs)
}
