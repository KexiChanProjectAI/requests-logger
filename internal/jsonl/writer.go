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

var (
	staleTimeout    = 5 * time.Minute
	cleanupInterval = 1 * time.Minute
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
	config   config.LogServerConfig
	handles  map[string]*fileHandle
	handlesMu sync.Mutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewWriter creates a new JSONL writer that writes to files in LogDir.
// Each hourly file is named <UTC-hour>.jsonl using the UTCHourlyLayout format.
// Stale file handles (not written to for 5 minutes) are automatically closed.
func NewWriter(cfg config.LogServerConfig) *Writer {
	w := &Writer{
		config:  cfg,
		handles: make(map[string]*fileHandle),
		stopCh:  make(chan struct{}),
	}
	w.wg.Add(1)
	go w.cleanupLoop()
	return w
}

// cleanupLoop periodically closes stale file handles.
func (w *Writer) cleanupLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(cleanupInterval)
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
func (w *Writer) closeStaleHandles() {
	w.handlesMu.Lock()
	defer w.handlesMu.Unlock()
	now := time.Now()
	for hour, fh := range w.handles {
		fh.mu.Lock()
		if now.Sub(fh.lastWrite) > staleTimeout {
			filePath := fh.filePath
			fh.file.Close()
			delete(w.handles, hour)
			fh.mu.Unlock()

			if w.config.ArchiveEnabled {
				go func(path string) {
					if _, err := archiver.ArchiveFile(path); err != nil {
						log.Printf("Failed to archive %s: %v", path, err)
					}
				}(filePath)
			}
		} else {
			fh.mu.Unlock()
		}
	}
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

	// Serialize writes to this file
	fh.mu.Lock()
	defer fh.mu.Unlock()

	fh.lastWrite = time.Now()

	// Marshal the record to JSON
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	// Write JSON line with newline
	_, err = fh.file.Write(append(data, '\n'))
	if err != nil {
		return fmt.Errorf("failed to write to %s: %w", filePath, err)
	}

	return nil
}

// getHandle returns the file handle for the given path, creating one if necessary.
func (w *Writer) getHandle(filePath string) *fileHandle {
	w.handlesMu.Lock()
	defer w.handlesMu.Unlock()

	// Check if we already have a handle
	if fh, ok := w.handles[filePath]; ok {
		return fh
	}

	// Open the file with append-only flags
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}

	fh := &fileHandle{
		file:      file,
		lastWrite: time.Now(),
		filePath:  filePath,
	}
	w.handles[filePath] = fh
	return fh
}

// Close closes all open file handles and stops the cleanup goroutine.
func (w *Writer) Close() error {
	// Signal the cleanup goroutine to stop
	close(w.stopCh)
	w.wg.Wait()

	// Close all file handles
	w.handlesMu.Lock()
	defer w.handlesMu.Unlock()

	var errs []error
	for path, fh := range w.handles {
		if err := fh.file.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close %s: %w", path, err))
		}
	}
	w.handles = make(map[string]*fileHandle)

	if len(errs) > 0 {
		return fmt.Errorf("errors closing files: %v", errs)
	}
	return nil
}
