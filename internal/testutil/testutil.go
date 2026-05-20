package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type UpstreamHandlerOpts struct {
	ResponseType  string
	StatusCode    int
	ResponseBody  interface{}
	SSEChunks     []string
	SSEDataFields []string
	DelayMs       int
	HandlerFunc   func(w http.ResponseWriter, r *http.Request)
}

type UpstreamRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type FakeUpstream struct {
	Server   *httptest.Server
	Requests []*UpstreamRequest
	mu       sync.Mutex
}

func NewFakeUpstream(opts UpstreamHandlerOpts) *FakeUpstream {
	if opts.ResponseType == "" {
		opts.ResponseType = "json"
	}
	if opts.StatusCode == 0 {
		opts.StatusCode = 200
	}

	fu := &FakeUpstream{
		Requests: make([]*UpstreamRequest, 0),
	}

	var handler http.HandlerFunc
	if opts.HandlerFunc != nil {
		handler = opts.HandlerFunc
	} else if opts.ResponseType == "sse" || len(opts.SSEChunks) > 0 || len(opts.SSEDataFields) > 0 {
		handler = fu.makeSSEHandler(opts)
	} else {
		handler = fu.makeJSONHandler(opts)
	}

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()

		fu.mu.Lock()
		fu.Requests = append(fu.Requests, &UpstreamRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Header: r.Header,
			Body:   body,
		})
		fu.mu.Unlock()

		r.Body = io.NopCloser(bytes.NewReader(body))
		handler.ServeHTTP(w, r)
	})

	fu.Server = httptest.NewServer(wrappedHandler)
	return fu
}

func (fu *FakeUpstream) makeJSONHandler(opts UpstreamHandlerOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body := opts.ResponseBody
		if body == nil {
			body = map[string]interface{}{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1234567890,
				"model":   "gpt-4o",
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": "test response",
						},
						"finish_reason": "stop",
					},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(opts.StatusCode)
		json.NewEncoder(w).Encode(body)
	}
}

func (fu *FakeUpstream) makeSSEHandler(opts UpstreamHandlerOpts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(opts.StatusCode)
		w.Write([]byte("\n"))

		flush := func(data string) {
			w.Write([]byte(data))
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		for _, chunk := range opts.SSEChunks {
			flush(fmt.Sprintf("data: %s\n\n", chunk))
		}

		for _, data := range opts.SSEDataFields {
			flush(fmt.Sprintf("data: %s\n\n", data))
		}

		flush("data: [DONE]\n\n")
	}
}

type CaptureLogRecord struct {
	Body   []byte
	Header http.Header
}

type FakeLogServer struct {
	Server   *httptest.Server
	Captures []*CaptureLogRecord
	mu       sync.Mutex
	path     string
}

func NewFakeLogServer() *FakeLogServer {
	fls := &FakeLogServer{
		Captures: make([]*CaptureLogRecord, 0),
		path:     "/logs",
	}

	fls.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != fls.path {
			http.NotFound(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusInternalServerError)
			return
		}
		r.Body.Close()

		fls.mu.Lock()
		fls.Captures = append(fls.Captures, &CaptureLogRecord{Body: body, Header: r.Header})
		fls.mu.Unlock()

		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("OK"))
	}))

	return fls
}

func (fls *FakeLogServer) URL() string {
	return fls.Server.URL
}

func (fls *FakeLogServer) GetCaptures() []*CaptureLogRecord {
	fls.mu.Lock()
	defer fls.mu.Unlock()
	result := make([]*CaptureLogRecord, len(fls.Captures))
	copy(result, fls.Captures)
	return result
}

func (fls *FakeLogServer) CaptureCount() int {
	fls.mu.Lock()
	defer fls.mu.Unlock()
	return len(fls.Captures)
}

func SSEChunkWriter(w io.Writer, data string) error {
	_, err := w.Write([]byte(fmt.Sprintf("data: %s\n\n", data)))
	return err
}

func SSEDoneWriter(w io.Writer) error {
	_, err := w.Write([]byte("data: [DONE]\n\n"))
	return err
}

func JSONResponseWriter(w http.ResponseWriter, statusCode int, v interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	return json.NewEncoder(w).Encode(v)
}

func NewTempJSONLDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "jsonl_test_*")
	if err != nil {
		t.Fatalf("failed to create temp JSONL dir: %v", err)
	}
	t.Cleanup(func() {
		os.RemoveAll(dir)
	})
	return dir
}

func ReadJSONLLines(t *testing.T, dir, path string) []map[string]interface{} {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatalf("failed to read JSONL file %s: %v", fullPath, err)
	}

	var records []map[string]interface{}
	lines := bytes.Split(data, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var record map[string]interface{}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("failed to parse JSONL line: %v", err)
		}
		records = append(records, record)
	}
	return records
}
