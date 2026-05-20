package sseutil_test

import (
	"io"
	"strings"
	"testing"

	"github.com/user/openai-go-proxy-logger/internal/sseutil"
)

func TestTeeReaderCapturesBytesReadSoFar(t *testing.T) {
	r := sseutil.NewTeeReader(strings.NewReader("data: one\n\ndata: two\n\n"))
	buf := make([]byte, 9)

	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read failed: %v", err)
	}
	if n == 0 {
		t.Fatal("expected bytes read")
	}

	if got := string(r.Captured()); got != string(buf[:n]) {
		t.Fatalf("expected captured %q, got %q", string(buf[:n]), got)
	}

	remaining, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remaining failed: %v", err)
	}
	want := string(buf[:n]) + string(remaining)
	if got := string(r.Captured()); got != want {
		t.Fatalf("expected all captured %q, got %q", want, got)
	}
}

func TestDataLinesExtractsDataAndDone(t *testing.T) {
	sse := []byte(": comment\nretry: 1\ndata: {\"delta\":\"hello\"}\n\ndata: [DONE]\n\n")

	lines := sseutil.DataLines(sse)
	if len(lines) != 2 {
		t.Fatalf("expected 2 data lines, got %d", len(lines))
	}
	if lines[0] != `{"delta":"hello"}` {
		t.Fatalf("unexpected first data line: %q", lines[0])
	}
	if lines[1] != "[DONE]" {
		t.Fatalf("unexpected done line: %q", lines[1])
	}
	if !sseutil.HasDone(sse) {
		t.Fatal("expected done frame to be detected")
	}
}

func TestReassembleDataLinesExcludesDone(t *testing.T) {
	sse := []byte("data: first\n\ndata: second\n\ndata: [DONE]\n\n")

	if got, want := sseutil.ReassembleDataLines(sse), "first\nsecond"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
