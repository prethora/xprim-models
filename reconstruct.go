package models

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// chunkStreamReader provides an io.Reader interface over a sequence of chunks.
// This allows files to be written without loading all chunks into memory.
// Chunks are deleted incrementally as they are fully consumed to minimize disk space usage.
type chunkStreamReader struct {
	// chunks is the ordered list of chunk hashes.
	chunks []string

	// cache provides access to chunk data.
	cache *chunkCache

	// currentChunk is the index of the current chunk being read.
	currentChunk int

	// currentData is the remaining data in the current chunk.
	currentData []byte

	// totalRead tracks bytes read across all chunks.
	totalRead int64

	// lastDeletedChunk tracks which chunks have been deleted.
	// All chunks with index < lastDeletedChunk have been deleted.
	lastDeletedChunk int
}

// newChunkStreamReader creates a reader that streams through the given chunks.
func newChunkStreamReader(chunks []string, cache *chunkCache) *chunkStreamReader {
	return &chunkStreamReader{
		chunks: chunks,
		cache:  cache,
	}
}

// Read implements io.Reader, reading sequentially through all chunks.
// Chunks are deleted incrementally as they are fully consumed.
func (r *chunkStreamReader) Read(p []byte) (n int, err error) {
	// If current buffer is empty, load next chunk
	for len(r.currentData) == 0 {
		if r.currentChunk >= len(r.chunks) {
			return 0, io.EOF
		}

		hash := r.chunks[r.currentChunk]
		data, ok := r.cache.get(hash)
		if !ok {
			return 0, fmt.Errorf("chunk %s not found in cache", hash[:min(8, len(hash))])
		}

		r.currentData = data
		r.currentChunk++

		// Delete the chunk that was previously in currentData (now fully consumed).
		// After incrementing currentChunk:
		//   - currentChunk-1 = chunk we just loaded (still in currentData, needed)
		//   - currentChunk-2 = chunk that was previously in currentData (fully consumed, deletable)
		if r.currentChunk >= 2 {
			prevChunkIdx := r.currentChunk - 2
			if prevChunkIdx >= r.lastDeletedChunk {
				prevHash := r.chunks[prevChunkIdx]
				r.cache.delete(prevHash) // Ignore errors - best effort cleanup
				r.lastDeletedChunk = prevChunkIdx + 1
			}
		}
	}

	// Copy from current buffer to output
	n = copy(p, r.currentData)
	r.currentData = r.currentData[n:]
	r.totalRead += int64(n)

	return n, nil
}

// deleteRemainingChunks cleans up any chunks not yet deleted.
// Call this after reconstruction completes successfully.
func (r *chunkStreamReader) deleteRemainingChunks() {
	for i := r.lastDeletedChunk; i < len(r.chunks); i++ {
		r.cache.delete(r.chunks[i]) // Ignore errors - best effort cleanup
	}
}

// Ensure chunkStreamReader implements io.Reader.
var _ io.Reader = (*chunkStreamReader)(nil)

// fileReconstructor assembles files from cached chunks.
type fileReconstructor struct {
	// chunkCache provides access to downloaded chunks.
	chunkCache *chunkCache

	// storage is used to write the reconstructed files.
	storage storageInterface
}

// newFileReconstructor creates a new file reconstructor.
func newFileReconstructor(cache *chunkCache, storage storageInterface) *fileReconstructor {
	return &fileReconstructor{
		chunkCache: cache,
		storage:    storage,
	}
}

// reconstruct extracts all files from the manifest using cached chunks.
// The chunks are read in order and streamed to the appropriate output files.
// The progressFn is called with the current file being processed.
func (f *fileReconstructor) reconstruct(ctx context.Context, mf manifest, ref ModelRef, progressFn func(currentFile string)) error {
	// Create chunk stream reader
	reader := newChunkStreamReader(mf.Chunks, f.chunkCache)

	// Get model directory
	modelDir := f.storage.modelPath(ref)

	// Ensure model directory exists
	if err := f.storage.ensureDir(modelDir); err != nil {
		return fmt.Errorf("creating model directory: %w", err)
	}

	// Extract each file
	for _, file := range mf.Files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Report progress
		if progressFn != nil {
			progressFn(file.Path)
		}

		// Create full path
		fullPath := filepath.Join(modelDir, file.Path)

		// Ensure parent directory exists
		parentDir := filepath.Dir(fullPath)
		if err := f.storage.ensureDir(parentDir); err != nil {
			return fmt.Errorf("creating directory for %s: %w", file.Path, err)
		}

		// Create output file
		outFile, err := os.Create(fullPath)
		if err != nil {
			return fmt.Errorf("creating file %s: %w", file.Path, err)
		}

		// Copy exact number of bytes
		written, err := io.CopyN(outFile, reader, file.Size)
		outFile.Close()

		if err != nil {
			return fmt.Errorf("writing file %s: %w", file.Path, err)
		}

		if written != file.Size {
			return fmt.Errorf("file %s: wrote %d bytes, expected %d", file.Path, written, file.Size)
		}
	}

	// Clean up any remaining cached chunks
	reader.deleteRemainingChunks()

	return nil
}
