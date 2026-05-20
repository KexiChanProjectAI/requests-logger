package sseutil

import (
	"bytes"
	"io"
	"strings"
	"sync"

	"github.com/tmaxmax/go-sse"
)

type TeeReader struct {
	r  io.Reader
	mu sync.Mutex
	b  bytes.Buffer
}

func NewTeeReader(r io.Reader) *TeeReader {
	return &TeeReader{r: r}
}

func (t *TeeReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if n > 0 {
		t.mu.Lock()
		_, _ = t.b.Write(p[:n])
		t.mu.Unlock()
	}
	return n, err
}

func (t *TeeReader) Captured() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]byte, t.b.Len())
	copy(out, t.b.Bytes())
	return out
}

func DataLines(sseData []byte) []string {
	var lines []string

	readEvents := sse.Read(bytes.NewReader(sseData), nil)
	readEvents(func(event sse.Event, err error) bool {
		if err != nil {
			return false
		}
		if len(event.Data) > 0 {
			lines = append(lines, event.Data)
		}
		return true
	})

	return lines
}

func ReassembleDataLines(sse []byte) string {
	var parts []string
	for _, line := range DataLines(sse) {
		if line == "[DONE]" {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func HasDone(sse []byte) bool {
	for _, line := range DataLines(sse) {
		if line == "[DONE]" {
			return true
		}
	}
	return false
}
