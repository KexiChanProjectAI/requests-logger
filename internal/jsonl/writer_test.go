package jsonl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"sync"
	"testing"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/archiver"
	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/testutil"
)

func TestWriteToCorrectHourlyFile(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006/01/02/15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	ts := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
	record := &logschema.Record{
		LogID:             "log-1",
		RequestID:         "req-1",
		Route:             "/v1/chat/completions",
		Method:            "POST",
		URL:               "https://api.openai.com/v1/chat/completions",
		RequestTimestamp:  ts,
		ResponseTimestamp: ts.Add(100 * time.Millisecond),
		TerminalStatus:    logschema.TerminalStatusCompleted,
	}

	err := w.Write(record)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedFile := filepath.Join(logDir, "2026/05/20/14.jsonl")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", expectedFile, err)
	}

	var readRecord logschema.Record
	if err := json.Unmarshal(data, &readRecord); err != nil {
		t.Fatalf("Invalid JSON in file: %v", err)
	}

	if readRecord.LogID != "log-1" {
		t.Errorf("Expected LogID 'log-1', got '%s'", readRecord.LogID)
	}
}

func TestConcurrentWrites(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006/01/02/15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	ts := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	numGoroutines := 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			record := &logschema.Record{
				LogID:             "log-concurrent",
				RequestID:         "req-concurrent",
				Route:             "/v1/chat/completions",
				Method:            "POST",
				URL:               "https://api.openai.com/v1/chat/completions",
				RequestTimestamp:  ts,
				ResponseTimestamp: ts.Add(100 * time.Millisecond),
				TerminalStatus:    logschema.TerminalStatusCompleted,
			}
			if err := w.Write(record); err != nil {
				t.Errorf("Write failed: %v", err)
			}
		}(i)
	}

	wg.Wait()

	lines := testutil.ReadJSONLLines(t, logDir, "2026/05/20/14.jsonl")
	if len(lines) != numGoroutines {
		t.Errorf("Expected %d records, got %d", numGoroutines, len(lines))
	}
}

func TestNewHourCreatesNewFile(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	ts1 := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
	record1 := &logschema.Record{
		LogID:            "log-1",
		RequestTimestamp: ts1,
	}
	if err := w.Write(record1); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	ts2 := time.Date(2026, 5, 20, 15, 30, 0, 0, time.UTC)
	record2 := &logschema.Record{
		LogID:            "log-2",
		RequestTimestamp: ts2,
	}
	if err := w.Write(record2); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	file1 := filepath.Join(logDir, "2026-05-20T14.jsonl")
	file2 := filepath.Join(logDir, "2026-05-20T15.jsonl")

	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Errorf("Expected file %s to exist", file1)
	}
	if _, err := os.Stat(file2); os.IsNotExist(err) {
		t.Errorf("Expected file %s to exist", file2)
	}

	lines1 := testutil.ReadJSONLLines(t, logDir, "2026-05-20T14.jsonl")
	lines2 := testutil.ReadJSONLLines(t, logDir, "2026-05-20T15.jsonl")

	if len(lines1) != 1 {
		t.Errorf("Expected 1 record in file1, got %d", len(lines1))
	}
	if len(lines2) != 1 {
		t.Errorf("Expected 1 record in file2, got %d", len(lines2))
	}
}

func TestValidJSONLFormat(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	ts := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	record := &logschema.Record{
		LogID:             "log-json-test",
		RequestID:         "req-json",
		Route:             "/v1/chat/completions",
		Method:            "POST",
		URL:               "https://api.openai.com/v1/chat/completions",
		RequestTimestamp:  ts,
		ResponseTimestamp: ts.Add(100 * time.Millisecond),
		TerminalStatus:    logschema.TerminalStatusCompleted,
		RequestHeaders:    map[string][]string{"Content-Type": {"application/json"}},
		ResponseHeaders:   map[string][]string{"Content-Type": {"application/json"}},
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(logDir, "2026-05-20T14.jsonl"))
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	lines := splitJSONLLines(data)
	if len(lines) != 1 {
		t.Errorf("Expected 1 line, got %d", len(lines))
	}

	for i, line := range lines {
		var parsed map[string]interface{}
		if err := json.Unmarshal(line, &parsed); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
	}
}

func splitJSONLLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	return lines
}

func TestCloseClosesAllHandles(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := NewWriter(cfg)

	ts := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		record := &logschema.Record{
			LogID:            "log-close-test",
			RequestTimestamp: ts.Add(time.Duration(i) * time.Hour),
		}
		if err := w.Write(record); err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	for path := range w.handles {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("File %s should be closed", path)
		}
	}
}

func TestStaleHandlesClosed(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := &Writer{
		config:      cfg,
		handles:     make(map[string]*fileHandle),
		stopCh:      make(chan struct{}),
		archiveOpts: archiver.Options{},
	}

	filePath := filepath.Join(logDir, "2026-05-20T14.jsonl")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	fh := &fileHandle{
		file:      file,
		lastWrite: time.Now().Add(-10 * time.Minute),
	}
	w.handles[filePath] = fh

	w.closeStaleHandles()

	if _, ok := w.handles[filePath]; ok {
		t.Errorf("Stale handle should have been closed")
	}
}

func TestWriteNilRecord(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	err := w.Write(nil)
	if err == nil {
		t.Error("Expected error for nil record")
	}
}

func TestWriteMultipleRecordsSameHour(t *testing.T) {
	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006-01-02T15",
	}

	w := NewWriter(cfg)
	defer w.Close()

	ts := time.Date(2026, 5, 20, 14, 0, 0, 0, time.UTC)
	numRecords := 10

	for i := 0; i < numRecords; i++ {
		record := &logschema.Record{
			LogID:             "log-multiple",
			RequestID:         "req-multiple",
			RequestTimestamp:  ts,
			ResponseTimestamp: ts.Add(time.Duration(i) * time.Second),
		}
		if err := w.Write(record); err != nil {
			t.Fatalf("Write %d failed: %v", i, err)
		}
	}

	lines := testutil.ReadJSONLLines(t, logDir, "2026-05-20T14.jsonl")
	if len(lines) != numRecords {
		t.Errorf("Expected %d records, got %d", numRecords, len(lines))
	}
}

func TestStaleFileArchived(t *testing.T) {
	origStaleTimeout := staleTimeout
	staleTimeout = 10 * time.Millisecond
	origCleanupInterval := cleanupInterval
	cleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		staleTimeout = origStaleTimeout
		cleanupInterval = origCleanupInterval
	})

	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006/01/02/15",
		ArchiveEnabled:  true,
	}

	w := NewWriter(cfg)

	ts := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
	record := &logschema.Record{
		LogID:             "log-archive-test",
		RequestID:         "req-archive-test",
		Route:             "/v1/chat/completions",
		Method:            "POST",
		URL:               "https://api.openai.com/v1/chat/completions",
		RequestTimestamp:  ts,
		ResponseTimestamp: ts.Add(100 * time.Millisecond),
		TerminalStatus:    logschema.TerminalStatusCompleted,
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedJSONL := filepath.Join(logDir, "2026/05/20/14.jsonl")
	expectedArchive := filepath.Join(logDir, "2026/05/20/14.tar.zst")

	time.Sleep(200 * time.Millisecond)

	for i := 0; i < 10; i++ {
		if _, err := os.Stat(expectedArchive); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := os.Stat(expectedJSONL); !os.IsNotExist(err) {
		t.Errorf("JSONL file should have been deleted, but still exists")
	}

	if _, err := os.Stat(expectedArchive); os.IsNotExist(err) {
		t.Errorf("Archive file should exist at %s", expectedArchive)
	}

	w.Close()
}

func TestArchiveDisabled(t *testing.T) {
	origStaleTimeout := staleTimeout
	staleTimeout = 10 * time.Millisecond
	origCleanupInterval := cleanupInterval
	cleanupInterval = 10 * time.Millisecond
	t.Cleanup(func() {
		staleTimeout = origStaleTimeout
		cleanupInterval = origCleanupInterval
	})

	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:          logDir,
		UTCHourlyLayout: "2006/01/02/15",
		ArchiveEnabled:  false,
	}

	w := NewWriter(cfg)

	ts := time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC)
	record := &logschema.Record{
		LogID:             "log-no-archive",
		RequestID:         "req-no-archive",
		Route:             "/v1/chat/completions",
		Method:            "POST",
		URL:               "https://api.openai.com/v1/chat/completions",
		RequestTimestamp:  ts,
		ResponseTimestamp: ts.Add(100 * time.Millisecond),
		TerminalStatus:    logschema.TerminalStatusCompleted,
	}

	if err := w.Write(record); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	expectedJSONL := filepath.Join(logDir, "2026/05/20/14.jsonl")
	expectedArchive := filepath.Join(logDir, "2026/05/20/14.tar.zst")

	time.Sleep(100 * time.Millisecond)

	w.Close()

	if _, err := os.Stat(expectedJSONL); os.IsNotExist(err) {
		t.Errorf("JSONL file should still exist when archiving is disabled")
	}

	if _, err := os.Stat(expectedArchive); !os.IsNotExist(err) {
		t.Errorf("Archive file should NOT exist when archiving is disabled")
	}
}

func TestCloseWaitsForQueuedArchives(t *testing.T) {
	origArchiveFile := archiveFile
	defer func() { archiveFile = origArchiveFile }()

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	archiveFile = func(path string, opts archiver.Options) (string, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return path + ".tar.zst", nil
	}

	logDir := t.TempDir()
	cfg := config.LogServerConfig{
		LogDir:               logDir,
		UTCHourlyLayout:      "2006-01-02T15",
		ArchiveEnabled:       true,
		ArchiveMaxConcurrent: 1,
	}

	w := &Writer{
		config:      cfg,
		handles:     make(map[string]*fileHandle),
		stopCh:      make(chan struct{}),
		archiveSem:  make(chan struct{}, 1),
		archiveOpts: archiver.Options{},
	}

	for i := 0; i < 2; i++ {
		filePath := filepath.Join(logDir, fmt.Sprintf("2026-05-20T1%d.jsonl", i))
		file, err := os.Create(filePath)
		if err != nil {
			t.Fatalf("create file: %v", err)
		}
		w.handles[filePath] = &fileHandle{
			file:      file,
			lastWrite: time.Now().Add(-10 * time.Minute),
			filePath:  filePath,
		}
	}

	w.closeStaleHandles()
	<-started

	closed := make(chan error, 1)
	go func() {
		closed <- w.Close()
	}()

	select {
	case err := <-closed:
		t.Fatalf("Close returned before archive jobs were released: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wait for queued archive jobs")
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 archive jobs, got %d", got)
	}
}
