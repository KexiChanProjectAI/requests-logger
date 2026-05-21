package archiver

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

var testOptions = Options{}

func TestArchiveFile(t *testing.T) {
	tmpDir := t.TempDir()

	jsonlPath := filepath.Join(tmpDir, "2026/05/20/14.jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	testContent := `{"log_id":"test-log","request_id":"test-req"}`
	if err := os.WriteFile(jsonlPath, []byte(testContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	archivePath, err := ArchiveFile(jsonlPath, testOptions)
	if err != nil {
		t.Fatalf("ArchiveFile failed: %v", err)
	}

	expectedArchive := strings.TrimSuffix(jsonlPath, ".jsonl") + ".tar.zst"
	if archivePath != expectedArchive {
		t.Errorf("expected archive path %s, got %s", expectedArchive, archivePath)
	}

	if _, err := os.Stat(archivePath); os.IsNotExist(err) {
		t.Fatalf("archive file was not created at %s", archivePath)
	}

	if _, err := os.Stat(jsonlPath); !os.IsNotExist(err) {
		t.Errorf("original jsonl file should have been deleted")
	}
}

func TestArchiveFileContent(t *testing.T) {
	tmpDir := t.TempDir()

	jsonlPath := filepath.Join(tmpDir, "2026/05/20/14.jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	testContent := `{"log_id":"test-log","request_id":"test-req"}`
	if err := os.WriteFile(jsonlPath, []byte(testContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	archivePath, err := ArchiveFile(jsonlPath, testOptions)
	if err != nil {
		t.Fatalf("ArchiveFile failed: %v", err)
	}

	f, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	decoder, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("create zstd reader: %v", err)
	}
	defer decoder.Close()

	tr := tar.NewReader(decoder)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}

	if header.Name != "14.jsonl" {
		t.Errorf("expected tar entry name 14.jsonl, got %s", header.Name)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("read tar content: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("expected content %q, got %q", testContent, string(content))
	}
}

func TestIsArchived(t *testing.T) {
	tmpDir := t.TempDir()

	jsonlPath := filepath.Join(tmpDir, "2026/05/20/14.jsonl")
	archivePath := strings.TrimSuffix(jsonlPath, ".jsonl") + ".tar.zst"

	if IsArchived(jsonlPath) {
		t.Errorf("IsArchived should return false when archive does not exist")
	}

	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("test"), 0644); err != nil {
		t.Fatalf("write archive file: %v", err)
	}

	if !IsArchived(jsonlPath) {
		t.Errorf("IsArchived should return true when archive exists")
	}
}

func TestArchiveFileAlreadyArchived(t *testing.T) {
	tmpDir := t.TempDir()

	jsonlPath := filepath.Join(tmpDir, "2026/05/20/14.jsonl")
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}

	testContent := `{"log_id":"test-log"}`
	if err := os.WriteFile(jsonlPath, []byte(testContent), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	archivePath := strings.TrimSuffix(jsonlPath, ".jsonl") + ".tar.zst"
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		t.Fatalf("mkdirall: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("existing"), 0644); err != nil {
		t.Fatalf("write archive file: %v", err)
	}

	resultPath, err := ArchiveFile(jsonlPath, testOptions)
	if err != nil {
		t.Fatalf("ArchiveFile should not fail when archive exists: %v", err)
	}

	if resultPath != archivePath {
		t.Errorf("expected returned path %s, got %s", archivePath, resultPath)
	}

	origContent, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatalf("read original file: %v", err)
	}
	if string(origContent) != testContent {
		t.Errorf("original file should not be deleted when archive exists")
	}
}

func TestArchiveFileNotFound(t *testing.T) {
	_, err := ArchiveFile("/nonexistent/path/14.jsonl", testOptions)
	if err == nil {
		t.Errorf("ArchiveFile should return error for nonexistent file")
	}
}

func TestNormalizeWindowSize(t *testing.T) {
	if got := normalizeWindowSize(512); got != zstd.MaxWindowSize {
		t.Fatalf("expected max window size %d, got %d", zstd.MaxWindowSize, got)
	}
	if got := normalizeWindowSize(300); got != 256<<20 {
		t.Fatalf("expected window size rounded down to 256 MiB, got %d", got)
	}
	if got := normalizeWindowSize(0); got != zstd.MaxWindowSize {
		t.Fatalf("expected zero to default to max window size, got %d", got)
	}
}
