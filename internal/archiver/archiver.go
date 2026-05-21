package archiver

import (
	"archive/tar"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

type Options struct {
	WindowSizeMB       int
	EncoderConcurrency int
}

// ArchiveFile compresses the given file into a .tar.zst archive using zstd max compression.
// The archive contains the file with its base name (no directory).
// After successful compression, the original file is deleted.
// Returns the path to the created archive.
func ArchiveFile(filePath string, opts Options) (string, error) {
	// Check if file exists
	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("stat file %s: %w", filePath, err)
	}

	// Check if already archived
	if IsArchived(filePath) {
		return strings.TrimSuffix(filePath, ".jsonl") + ".tar.zst", nil
	}

	// Create archive path: replace .jsonl with .tar.zst
	archivePath := strings.TrimSuffix(filePath, ".jsonl") + ".tar.zst"

	// Open source file
	src, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("open source file: %w", err)
	}
	defer src.Close()

	// Create destination archive file
	dst, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive file: %w", err)
	}
	defer dst.Close()

	// Create zstd encoder with max compression (level 22)
	encoder, err := zstd.NewWriter(
		dst,
		zstd.WithEncoderLevel(zstd.SpeedBestCompression),
		zstd.WithWindowSize(normalizeWindowSize(opts.WindowSizeMB)),
		zstd.WithEncoderConcurrency(normalizeEncoderConcurrency(opts.EncoderConcurrency)),
	)
	if err != nil {
		return "", fmt.Errorf("create zstd encoder: %w", err)
	}
	defer encoder.Close()

	// Create tar writer
	tw := tar.NewWriter(encoder)
	defer tw.Close()

	// Write tar header
	header := &tar.Header{
		Name:    filepath.Base(filePath),
		Mode:    int64(info.Mode()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return "", fmt.Errorf("write tar header: %w", err)
	}

	// Copy file content to tar
	if _, err := io.Copy(tw, src); err != nil {
		return "", fmt.Errorf("copy file to tar: %w", err)
	}

	// Close tar writer to flush
	if err := tw.Close(); err != nil {
		return "", fmt.Errorf("close tar writer: %w", err)
	}

	// Close zstd encoder to flush
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("close zstd encoder: %w", err)
	}

	if err := os.Remove(filePath); err != nil {
		return "", fmt.Errorf("delete original file: %w", err)
	}

	return archivePath, nil
}

func normalizeWindowSize(windowSizeMB int) int {
	if windowSizeMB <= 0 {
		return zstd.MaxWindowSize
	}

	windowSizeBytes := windowSizeMB << 20
	if windowSizeBytes < zstd.MinWindowSize {
		return zstd.MinWindowSize
	}
	if windowSizeBytes > zstd.MaxWindowSize {
		return zstd.MaxWindowSize
	}
	if windowSizeBytes&(windowSizeBytes-1) == 0 {
		return windowSizeBytes
	}

	return 1 << (bits.Len(uint(windowSizeBytes)) - 1)
}

func normalizeEncoderConcurrency(concurrency int) int {
	if concurrency < 0 {
		return 0
	}
	return concurrency
}

// IsArchived checks if a .tar.zst archive exists for the given base file path.
func IsArchived(filePath string) bool {
	archivePath := strings.TrimSuffix(filePath, ".jsonl") + ".tar.zst"
	_, err := os.Stat(archivePath)
	return err == nil
}
