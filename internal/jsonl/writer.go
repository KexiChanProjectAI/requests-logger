package jsonl

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/archiver"
	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
)

// Package-level defaults for stale handle timeout and cleanup interval.
// These can be overridden by config values, but tests may set these directly.
var (
	staleTimeout    = 5 * time.Minute
	cleanupInterval = 1 * time.Minute
	archiveFile     = archiver.ArchiveFile
)

// fileHandle holds an open file and its last-write timestamp.
type fileHandle struct {
	file      *os.File
	mu        sync.Mutex
	lastWrite time.Time
	filePath  string
}

// Writer writes log records to hourly JSONL files.
type Writer struct {
	config      config.LogServerConfig
	handles     map[string]*fileHandle
	handlesMu   sync.RWMutex
	stopCh      chan struct{}
	archiveSem  chan struct{}
	archiveOpts archiver.Options
	cleanupWG   sync.WaitGroup
	archiveWG   sync.WaitGroup
}

// NewWriter creates a new JSONL writer that writes to files in LogDir.
// Each hourly file is named <UTC-hour>.jsonl using the UTCHourlyLayout format.
// Stale file handles (not written to for StaleHandleTimeout) are automatically closed.
func NewWriter(cfg config.LogServerConfig) *Writer {
	// Apply defaults from package-level vars if not explicitly set in config.
	if cfg.StaleHandleTimeout <= 0 {
		cfg.StaleHandleTimeout = staleTimeout
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = cleanupInterval
	}

	w := &Writer{
		config:  cfg,
		handles: make(map[string]*fileHandle),
		stopCh:  make(chan struct{}),
		archiveOpts: archiver.Options{
			WindowSizeMB:       cfg.ArchiveZstdWindowMB,
			EncoderConcurrency: cfg.ArchiveZstdConcurrency,
		},
	}
	if cfg.ArchiveMaxConcurrent > 0 {
		w.archiveSem = make(chan struct{}, cfg.ArchiveMaxConcurrent)
	}
	w.cleanupWG.Add(1)
	go w.cleanupLoop()
	return w
}

// cleanupLoop periodically closes stale file handles.
func (w *Writer) cleanupLoop() {
	defer w.cleanupWG.Done()
	ticker := time.NewTicker(w.config.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.closeStaleHandles()
		case <-w.stopCh:
			return
		}
	}
}

// closeStaleHandles closes file handles that haven't been written to in staleTimeout.
// Lock is released before closing files to avoid blocking new writes during I/O.
func (w *Writer) closeStaleHandles() {
	w.handlesMu.Lock()
	now := time.Now()

	// Build list of handles to close and archive under lock
	type staleHandle struct {
		hour     string
		fh       *fileHandle
		filePath string
	}
	var stale []staleHandle
	for hour, fh := range w.handles {
		fh.mu.Lock()
		if now.Sub(fh.lastWrite) > w.config.StaleHandleTimeout {
			stale = append(stale, staleHandle{hour: hour, fh: fh, filePath: fh.filePath})
		}
		fh.mu.Unlock()
	}

	// Remove from map while still holding lock
	for _, s := range stale {
		delete(w.handles, s.hour)
	}
	w.handlesMu.Unlock()

	// Close files and trigger archives OUTSIDE the lock
	var archivePaths []string
	for _, s := range stale {
		s.fh.mu.Lock()
		s.fh.file.Close()
		s.fh.mu.Unlock()
		if w.config.ArchiveEnabled {
			archivePaths = append(archivePaths, s.filePath)
		}
	}

	for _, path := range archivePaths {
		w.startArchive(path)
	}
}

// startArchive starts archiving a file in a background goroutine.
// If the archive semaphore is full (non-blocking), it skips archiving and logs a warning.
func (w *Writer) startArchive(path string) {
	w.archiveWG.Add(1)
	go func() {
		defer w.archiveWG.Done()

		// Non-blocking acquisition of archive semaphore slot.
		// If the semaphore is full, skip archiving rather than blocking the cleanup goroutine.
		if w.archiveSem != nil {
			select {
			case w.archiveSem <- struct{}{}:
				// acquired slot
			default:
				// semaphore full, skip archiving
				log.Printf("archive limit reached, skipping %s", path)
				return
			}
			defer func() { <-w.archiveSem }()
		}

		if _, err := archiveFile(path, w.archiveOpts); err != nil {
			log.Printf("Failed to archive %s: %v", path, err)
		}
	}()
}

// filePathForTime returns the JSONL file path for the given UTC time.
func (w *Writer) filePathForTime(t time.Time) string {
	filename := t.UTC().Format(w.config.UTCHourlyLayout) + ".jsonl"
	fullPath := filepath.Join(w.config.LogDir, filename)
	dir := filepath.Dir(fullPath)
	os.MkdirAll(dir, 0755)
	return fullPath
}

// Write writes a log record to the appropriate hourly JSONL file.
// The file is determined by the record's RequestTimestamp in UTC.
// If the UTC hour has changed since the last write, a new file handle is created.
// JSON marshaling is performed outside the per-file lock to reduce lock hold time.
func (w *Writer) Write(record *logschema.Record) error {
	if record == nil {
		return fmt.Errorf("record is nil")
	}

	// Determine the file path based on the record's timestamp
	filePath := w.filePathForTime(record.RequestTimestamp)

	// Get or create the file handle
	fh := w.getHandle(filePath)
	if fh == nil {
		return fmt.Errorf("failed to get file handle for %s", filePath)
	}

	// Marshal the record to JSON BEFORE acquiring lock to reduce lock hold time
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Serialize writes to this file
	fh.mu.Lock()
	defer fh.mu.Unlock()

	fh.lastWrite = time.Now()

	// Write JSON line with newline
	_, err = fh.file.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", filePath, err)
	}

	return nil
}

// getHandle returns the file handle for the given path, creating one if necessary.
// Uses RWMutex for better concurrent read performance.
func (w *Writer) getHandle(filePath string) *fileHandle {
	// Fast path: use read lock for existing handles
	w.handlesMu.RLock()
	fh, ok := w.handles[filePath]
	w.handlesMu.RUnlock()
	if ok {
		return fh
	}

	// Slow path: acquire write lock to create new handle
	w.handlesMu.Lock()
	defer w.handlesMu.Unlock()

	// Double-check after acquiring write lock
	if fh, ok = w.handles[filePath]; ok {
		return fh
	}

	// Open the file with append-only flags
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}

	fh = &fileHandle{
		file:      file,
		lastWrite: time.Now(),
		filePath:  filePath,
	}
	w.handles[filePath] = fh
	return fh
}

// Close closes all open file handles and stops the cleanup goroutine.
// It waits for in-flight archives with a timeout to prevent indefinite hang.
func (w *Writer) Close() error {
	// Signal the cleanup goroutine to stop
	close(w.stopCh)
	w.cleanupWG.Wait()

	// Close all file handles
	w.handlesMu.Lock()
	var errs []error
	for path, fh := range w.handles {
		if err := fh.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s: %w", path, err))
		}
	}
	w.handles = make(map[string]*fileHandle)
	w.handlesMu.Unlock()

	// Wait for archive goroutines to complete with timeout.
	// This prevents Close from hanging indefinitely if an archive gets stuck.
	archiveDone := make(chan struct{})
	go func() {
		w.archiveWG.Wait()
		close(archiveDone)
	}()

	select {
	case <-archiveDone:
		// all archives completed
	case <-time.After(5 * time.Second):
		log.Printf("archive wait timeout, proceeding with close")
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing files: %v", errs)
	}
	return nil
}
