package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// ChunkCacheMaxAge is the maximum age for cached chunks before cleanup.
const ChunkCacheMaxAge = 24 * time.Hour

// verifyHash computes the SHA-256 hash of data and compares it to expectedHash.
// Returns nil if the hash matches, ErrHashMismatch if verification fails.
// The expectedHash should be a lowercase hex-encoded string.
func verifyHash(data []byte, expectedHash string) error {
	h := sha256.Sum256(data)
	actual := hex.EncodeToString(h[:])
	if actual != expectedHash {
		return ErrHashMismatch
	}
	return nil
}

// chunkJob represents a unit of work for the download worker pool.
type chunkJob struct {
	// hash is the SHA-256 hash of the chunk to download.
	hash string

	// index is the position of this chunk in the sequence.
	index int

	// size is the expected size of the chunk in bytes.
	size int64
}

// downloadResult contains the result of a chunk download operation.
type downloadResult struct {
	// index identifies which chunk this result is for.
	index int

	// err is nil on success, or the error that occurred.
	err error

	// bytesDownloaded is the bytes fetched from network (0 if from cache).
	bytesDownloaded int64
}

// chunkCache manages temporary storage of downloaded chunks.
type chunkCache struct {
	// cacheDir is the directory where chunks are stored.
	cacheDir string

	// mu protects cache operations.
	mu sync.RWMutex
}

// newChunkCache creates a new chunk cache in the specified directory.
func newChunkCache(cacheDir string) *chunkCache {
	return &chunkCache{
		cacheDir: cacheDir,
	}
}

// chunkPath returns the full path for a chunk file.
func (c *chunkCache) chunkPath(hash string) string {
	return filepath.Join(c.cacheDir, hash)
}

// get retrieves a cached chunk by hash.
// Returns the chunk data and true if found, nil and false if not cached.
func (c *chunkCache) get(hash string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	path := c.chunkPath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// put stores a chunk in the cache.
// The chunk is stored with its hash as the filename.
func (c *chunkCache) put(hash string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Ensure cache directory exists
	if err := os.MkdirAll(c.cacheDir, 0755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}

	path := c.chunkPath(hash)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing chunk to cache: %w", err)
	}
	return nil
}

// delete removes a chunk from the cache.
func (c *chunkCache) delete(hash string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	path := c.chunkPath(hash)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting chunk from cache: %w", err)
	}
	return nil
}

// cleanup removes all cached chunks older than maxAge.
func (c *chunkCache) cleanup(maxAge time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading cache dir: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(c.cacheDir, entry.Name())
			os.Remove(path) // Ignore errors for individual file removals
		}
	}

	return nil
}

// downloadEngine orchestrates the parallel download of chunks.
type downloadEngine struct {
	// registry is used to fetch chunks from the remote registry.
	registry *registryClient

	// storage provides access to the chunk cache.
	storage storageInterface

	// logger receives diagnostic messages. May be nil.
	logger Logger

	// errChan receives the first fatal error from workers.
	// Buffered with capacity 1 to prevent goroutine leaks.
	errChan chan error

	// wg tracks active download workers for graceful shutdown.
	wg sync.WaitGroup

	// bytesInProgress tracks bytes currently being downloaded across all workers.
	// Updated atomically as bytes are read from the network.
	bytesInProgress int64
}

// newDownloadEngine creates a new download engine.
func newDownloadEngine(registry *registryClient, storage storageInterface, logger Logger) *downloadEngine {
	return &downloadEngine{
		registry: registry,
		storage:  storage,
		logger:   logger,
		errChan:  make(chan error, 1),
	}
}

// downloadChunks downloads all specified chunks with parallel workers.
// The progressFn is called after each chunk completes with (completed, total, bytesDownloaded, bytesInProgress).
// bytesDownloaded is the cumulative bytes fetched from network (excludes cache hits).
// bytesInProgress is the bytes currently being downloaded across all workers (for smooth speed calc).
// Chunks are cached to avoid re-downloading on retry or partial failure.
func (d *downloadEngine) downloadChunks(ctx context.Context, chunks []string, concurrency int, progressFn func(completed, total int, bytesDownloaded, bytesInProgress int64)) error {
	if len(chunks) == 0 {
		return nil
	}

	// Reset bytesInProgress for this download session
	atomic.StoreInt64(&d.bytesInProgress, 0)

	cache := newChunkCache(d.storage.chunkCachePath())

	// Create channels
	jobs := make(chan chunkJob, len(chunks))
	results := make(chan downloadResult, len(chunks))

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start workers
	for i := 0; i < concurrency; i++ {
		d.wg.Add(1)
		go d.worker(ctx, cache, jobs, results)
	}

	// Send jobs
	go func() {
		for i, hash := range chunks {
			select {
			case jobs <- chunkJob{hash: hash, index: i}:
			case <-ctx.Done():
				break
			}
		}
		close(jobs)
	}()

	// Collect results
	var firstErr error
	completed := 0
	total := len(chunks)
	var totalBytesDownloaded int64

	// Start a ticker to report progress periodically (for smooth bytesInProgress updates)
	var progressTicker *time.Ticker
	var progressDone chan struct{}
	if progressFn != nil {
		progressTicker = time.NewTicker(time.Second)
		progressDone = make(chan struct{})
		go func() {
			for {
				select {
				case <-progressTicker.C:
					progressFn(completed, total, totalBytesDownloaded, atomic.LoadInt64(&d.bytesInProgress))
				case <-progressDone:
					return
				}
			}
		}()
	}

resultLoop:
	for completed < total {
		select {
		case result := <-results:
			if result.err != nil && firstErr == nil {
				firstErr = result.err
				cancel() // Signal workers to stop
			}
			completed++
			totalBytesDownloaded += result.bytesDownloaded
			if progressFn != nil {
				progressFn(completed, total, totalBytesDownloaded, atomic.LoadInt64(&d.bytesInProgress))
			}
		case <-ctx.Done():
			if firstErr == nil {
				firstErr = ctx.Err()
			}
			break resultLoop
		}
	}

	// Stop progress ticker
	if progressTicker != nil {
		progressTicker.Stop()
		close(progressDone)
	}

	// Wait for workers to finish
	d.wg.Wait()

	return firstErr
}

// worker processes chunk download jobs.
func (d *downloadEngine) worker(ctx context.Context, cache *chunkCache, jobs <-chan chunkJob, results chan<- downloadResult) {
	defer d.wg.Done()

	for {
		select {
		case job, ok := <-jobs:
			if !ok {
				return
			}

			bytesDownloaded, err := d.downloadChunk(ctx, cache, job)
			select {
			case results <- downloadResult{index: job.index, err: err, bytesDownloaded: bytesDownloaded}:
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// downloadChunk downloads a single chunk, using cache if available.
// Returns (bytesDownloaded, error) where bytesDownloaded is 0 for cache hits.
func (d *downloadEngine) downloadChunk(ctx context.Context, cache *chunkCache, job chunkJob) (int64, error) {
	// Check cache first
	if data, ok := cache.get(job.hash); ok {
		// Verify cached data
		if err := verifyHash(data, job.hash); err == nil {
			if d.logger != nil {
				d.logger.Debug("chunk cache hit", "hash", job.hash[:8])
			}
			return 0, nil // Cache hit, no bytes downloaded
		}
		// Cache corrupted, delete and re-download
		cache.delete(job.hash)
	}

	// Track bytes read during this download for bytesInProgress
	var bytesReadThisChunk int64

	// Download from registry with progress tracking
	data, err := d.registry.fetchChunkWithProgress(ctx, job.hash, func(delta int64) {
		atomic.AddInt64(&d.bytesInProgress, delta)
		bytesReadThisChunk += delta
	})

	// When chunk completes (success or failure), remove from in-progress
	// This must happen BEFORE we return, so the bytes transition cleanly
	if bytesReadThisChunk > 0 {
		atomic.AddInt64(&d.bytesInProgress, -bytesReadThisChunk)
	}

	if err != nil {
		return 0, fmt.Errorf("downloading chunk %s: %w", job.hash[:8], err)
	}

	// Verify hash
	if err := verifyHash(data, job.hash); err != nil {
		return 0, fmt.Errorf("chunk %s: %w", job.hash[:8], err)
	}

	// Store in cache
	if err := cache.put(job.hash, data); err != nil {
		return 0, fmt.Errorf("caching chunk %s: %w", job.hash[:8], err)
	}

	if d.logger != nil {
		d.logger.Debug("chunk downloaded", "hash", job.hash[:8], "size", len(data))
	}

	return int64(len(data)), nil // Return actual bytes downloaded
}
