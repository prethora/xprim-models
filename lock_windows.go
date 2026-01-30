//go:build windows

package models

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Locker provides mutual exclusion for file operations.
type Locker interface {
	// Lock acquires an exclusive lock on the file.
	// Blocks until lock is acquired or timeout expires.
	// Returns error if lock cannot be acquired within timeout.
	Lock() error

	// Unlock releases the lock.
	// Safe to call multiple times.
	Unlock() error
}

// fileLock implements Locker using LockFileEx() mandatory locking on Windows.
type fileLock struct {
	// file is the lock file handle.
	file *os.File

	// timeout is the maximum duration to wait for lock acquisition.
	timeout time.Duration

	// locked tracks whether the lock is currently held.
	locked bool
}

// newFileLock creates a new file lock for the given path.
// Creates the lock file if it doesn't exist.
func newFileLock(path string, timeout time.Duration) (*fileLock, error) {
	// Open or create the lock file
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	return &fileLock{
		file:    file,
		timeout: timeout,
	}, nil
}

// Lock acquires an exclusive mandatory lock using LockFileEx().
// Uses polling with backoff to implement timeout behavior.
func (l *fileLock) Lock() error {
	if l.locked {
		return nil // Already locked
	}

	deadline := time.Now().Add(l.timeout)
	sleepDuration := 10 * time.Millisecond

	for {
		// Try non-blocking lock
		err := windows.LockFileEx(
			windows.Handle(l.file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1, 0,
			&windows.Overlapped{},
		)
		if err == nil {
			l.locked = true
			return nil
		}

		// Check if we've exceeded the timeout
		if time.Now().After(deadline) {
			return fmt.Errorf("lock timeout after %v", l.timeout)
		}

		// Wait before retrying (with backoff)
		time.Sleep(sleepDuration)
		if sleepDuration < 100*time.Millisecond {
			sleepDuration *= 2
		}
	}
}

// Unlock releases the mandatory lock and closes the file handle.
func (l *fileLock) Unlock() error {
	if !l.locked {
		// Close the file even if not locked
		if l.file != nil {
			l.file.Close()
			l.file = nil
		}
		return nil
	}

	var unlockErr error
	if l.file != nil {
		unlockErr = windows.UnlockFileEx(
			windows.Handle(l.file.Fd()),
			0,
			1, 0,
			&windows.Overlapped{},
		)
		l.file.Close()
		l.file = nil
	}
	l.locked = false

	return unlockErr
}
