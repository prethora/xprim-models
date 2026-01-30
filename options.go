package models

import (
	"net/http"
	"time"
)

// Concurrency constants for chunk downloads.
const (
	// DefaultConcurrency is the default number of concurrent chunk downloads.
	DefaultConcurrency = 4

	// MaxConcurrency is the maximum allowed concurrent chunk downloads.
	MaxConcurrency = 16

	// DefaultRequestTimeout is the default timeout for HTTP requests.
	DefaultRequestTimeout = 30 * time.Second
)

// Retry configuration constants for failed HTTP requests.
const (
	// MaxRetries is the maximum number of retry attempts for failed requests.
	MaxRetries = 3

	// InitialBackoff is the initial backoff duration before first retry.
	InitialBackoff = 1 * time.Second

	// MaxBackoff is the maximum backoff duration between retries.
	MaxBackoff = 4 * time.Second
)

// PullOption configures a pull operation.
type PullOption func(*pullConfig)

// pullConfig holds configuration for a pull operation.
type pullConfig struct {
	// force causes re-download even if model is already installed.
	force bool

	// concurrency is the number of concurrent chunk downloads.
	concurrency int

	// progressFn is called with progress updates during download.
	progressFn func(PullProgress)
}

// newPullConfig returns a pullConfig with default values.
func newPullConfig() *pullConfig {
	return &pullConfig{
		concurrency: DefaultConcurrency,
	}
}

// WithForce forces re-download even if the model is already installed.
func WithForce() PullOption {
	return func(c *pullConfig) {
		c.force = true
	}
}

// WithConcurrency sets the number of concurrent chunk downloads.
// Values are clamped to the range [1, MaxConcurrency].
// Default is DefaultConcurrency (4).
func WithConcurrency(n int) PullOption {
	return func(c *pullConfig) {
		if n < 1 {
			n = 1
		}
		if n > MaxConcurrency {
			n = MaxConcurrency
		}
		c.concurrency = n
	}
}

// WithProgress sets a callback for progress updates during download.
// The callback is invoked from download worker goroutines and must be thread-safe.
func WithProgress(fn func(PullProgress)) PullOption {
	return func(c *pullConfig) {
		c.progressFn = fn
	}
}

// ManagerOption configures a Manager.
type ManagerOption func(*managerConfig)

// managerConfig holds configuration for Manager construction.
type managerConfig struct {
	// httpClient is used for all HTTP requests to the registry.
	httpClient HTTPClient

	// logger receives diagnostic log messages.
	logger Logger
}

// newManagerConfig returns a managerConfig with default values.
func newManagerConfig() *managerConfig {
	return &managerConfig{
		httpClient: http.DefaultClient,
	}
}

// WithHTTPClient sets a custom HTTP client for registry requests.
// Useful for testing with mock servers or customizing timeouts.
// If not set, http.DefaultClient is used.
func WithHTTPClient(client HTTPClient) ManagerOption {
	return func(c *managerConfig) {
		c.httpClient = client
	}
}

// WithLogger sets a logger for diagnostic output.
// If not set, logging is disabled.
func WithLogger(logger Logger) ManagerOption {
	return func(c *managerConfig) {
		c.logger = logger
	}
}

// HTTPClient is the interface for HTTP operations.
// *http.Client satisfies this interface.
type HTTPClient interface {
	// Do sends an HTTP request and returns an HTTP response.
	Do(req *http.Request) (*http.Response, error)
}

// Logger is the interface for diagnostic logging.
// Compatible with slog, zap, logrus, and other structured loggers.
type Logger interface {
	// Debug logs a debug-level message with optional key-value pairs.
	Debug(msg string, keysAndValues ...any)

	// Info logs an info-level message with optional key-value pairs.
	Info(msg string, keysAndValues ...any)

	// Warn logs a warning-level message with optional key-value pairs.
	Warn(msg string, keysAndValues ...any)

	// Error logs an error-level message with optional key-value pairs.
	Error(msg string, keysAndValues ...any)
}
