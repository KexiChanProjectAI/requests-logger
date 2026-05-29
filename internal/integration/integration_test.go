package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/jsonl"
	"github.com/user/openai-go-proxy-logger/internal/logclient"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/logserver"
	"github.com/user/openai-go-proxy-logger/internal/proxy"
	"github.com/user/openai-go-proxy-logger/internal/testutil"
)

const testToken = "test-token-123"

type testHarness struct {
	t              *testing.T
	tmpDir         string
	fakeUpstream   *testutil.FakeUpstream
	logServer      *httptest.Server
	logServerURL   string
	jsonlWriter    *jsonl.Writer
	logClient     *logclient.Client
	proxyHandler   http.Handler
	proxyServer   *httptest.Server
	mu             sync.Mutex
}

func newTestHarness(t *testing.T, upstreamOpts testutil.UpstreamHandlerOpts) *testHarness {
	h := &testHarness{t: t}
	h.t.Helper()

	// 1. Create temp dir for JSONL output
	var err error
	h.tmpDir, err = os.MkdirTemp("", "jsonl-test-*")
	if err != nil {
		h.t.Fatalf("failed to create temp dir: %v", err)
	}

	// 2. Create FakeUpstream
	h.fakeUpstream = testutil.NewFakeUpstream(upstreamOpts)
	t.Cleanup(func() { h.fakeUpstream.Server.Close() })

	// 3. Create real JSONL Writer with temp dir
	logServerCfg := config.LogServerConfig{
		LogDir:           h.tmpDir,
		UTCHourlyLayout:  "2006/01/02/15",
		LogServerToken:   testToken,
	}
	h.jsonlWriter = jsonl.NewWriter(logServerCfg)
	t.Cleanup(func() { h.jsonlWriter.Close() })

	// 4. Create real log server handler with the writer
	logServerHandler := logserver.NewHandler(logServerCfg, h.jsonlWriter)
	h.logServer = httptest.NewServer(logServerHandler)
	h.logServerURL = h.logServer.URL
	t.Cleanup(func() { h.logServer.Close() })

	// 5. Create real log client pointing at log server
	proxyCfg := config.ProxyConfig{
		UpstreamBaseURL: h.fakeUpstream.Server.URL,
		LogServerURL:    h.logServerURL,
		LogServerToken:  testToken,
		LogQueueSize:    100,
	}
	h.logClient = logclient.NewClient(proxyCfg)

	// 6. Start log client background worker
	h.logClient.Start()
	t.Cleanup(func() { h.logClient.Stop() })

	// 7. Create proxy handler with log client as enqueuer
	h.proxyHandler = proxy.NewHandler(proxyCfg, h.logClient)

	// 8. Create proxy server
	h.proxyServer = httptest.NewServer(h.proxyHandler)
	t.Cleanup(func() { h.proxyServer.Close() })

	return h
}

func (h *testHarness) doRequest(method, route string, body interface{}) (*http.Response, string) {
	var bodyReader io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(bs)
	}
	req, err := http.NewRequest(method, h.proxyServer.URL+route, bodyReader)
	if err != nil {
		h.t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("failed to do request: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	return resp, string(bodyBytes)
}

func (h *testHarness) waitForJSONL() error {
	// Wait for log client to deliver records
	time.Sleep(200 * time.Millisecond)
	return nil
}

func (h *testHarness) readJSONLRecords() ([]*logschema.Record, error) {
	var records []*logschema.Record

	err := filepath.WalkDir(h.tmpDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			var rec logschema.Record
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				return fmt.Errorf("failed to unmarshal record from %s: %w", path, err)
			}
			records = append(records, &rec)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk dir: %w", err)
	}

	return records, nil
}

func (h *testHarness) verifyRecord(t *testing.T, rec *logschema.Record, expectedRoute string, expectedStream bool, expectedUpstreamStatus int) {
	t.Helper()

	if rec.LogID == "" {
		t.Error("log_id should not be empty")
	}
	if rec.Route != expectedRoute {
		t.Errorf("route = %s, want %s", rec.Route, expectedRoute)
	}
	if rec.Method != "POST" {
		t.Errorf("method = %s, want POST", rec.Method)
	}
	if rec.UpstreamStatus != expectedUpstreamStatus {
		t.Errorf("upstream_status = %d, want %d", rec.UpstreamStatus, expectedUpstreamStatus)
	}
	if rec.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Errorf("terminal_status = %s, want %s", rec.TerminalStatus, logschema.TerminalStatusCompleted)
	}
	if rec.Stream != expectedStream {
		t.Errorf("stream = %v, want %v", rec.Stream, expectedStream)
	}
	if rec.RequestID != "" {
		// RequestID comes from x-request-id header which we didn't send
	}
}

func (h *testHarness) verifyHeaders(t *testing.T, rec *logschema.Record) {
	t.Helper()

	// Verify Authorization header is present
	authHeaders, ok := rec.RequestHeaders["Authorization"]
	if !ok || len(authHeaders) == 0 {
		t.Error("Authorization header should be present in request_headers")
	}
	if ok && len(authHeaders) > 0 && authHeaders[0] != "Bearer test-api-key" {
		t.Errorf("Authorization header = %s, want Bearer test-api-key", authHeaders[0])
	}

	// Verify Content-Type header
	ctHeaders, ok := rec.RequestHeaders["Content-Type"]
	if !ok || len(ctHeaders) == 0 {
		t.Error("Content-Type header should be present in request_headers")
	}
}

func TestNonStreamingChatCompletions(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello from chat",
					},
					"finish_reason": "stop",
				},
			},
		},
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	resp, _ := h.doRequest("POST", "/v1/chat/completions", body)

	// Verify response headers have x-proxy-log-id
	logID := resp.Header.Get("x-proxy-log-id")
	if logID == "" {
		t.Error("x-proxy-log-id header should be present in response")
	}

	// Wait for log delivery
	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	// Read and verify JSONL records
	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]
	h.verifyRecord(t, rec, "/v1/chat/completions", false, 200)
	h.verifyHeaders(t, rec)

	// Verify request body
	if len(rec.RequestBody) == 0 {
		t.Error("request_body should not be empty")
	}

	// Verify response body (non-streaming exact JSON)
	if len(rec.ResponseBody) == 0 {
		t.Error("response_body should not be empty")
	}
	// Check it's valid JSON
	var respParsed map[string]interface{}
	if err := json.Unmarshal(rec.ResponseBody, &respParsed); err != nil {
		t.Errorf("response_body should be valid JSON: %v", err)
	}
}

func TestNonStreamingResponses(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"id":           "resp_test_123",
			"object":       "response",
			"status":       "completed",
			"model":        "gpt-4o",
			"output_text":  "Hello from responses",
		},
	})

	body := map[string]interface{}{
		"model":    "gpt-4o",
		"input":    "Hello",
		"stream":   false,
	}

	resp, _ := h.doRequest("POST", "/v1/responses", body)

	logID := resp.Header.Get("x-proxy-log-id")
	if logID == "" {
		t.Error("x-proxy-log-id header should be present in response")
	}

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]
	h.verifyRecord(t, rec, "/v1/responses", false, 200)
	h.verifyHeaders(t, rec)

	if len(rec.RequestBody) == 0 {
		t.Error("request_body should not be empty")
	}
	if len(rec.ResponseBody) == 0 {
		t.Error("response_body should not be empty")
	}
}

func TestStreamingChatCompletions(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType:  "sse",
		StatusCode:    200,
		SSEDataFields: []string{`{"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"}}]}`, `{"id":"chatcmpl-2","choices":[{"delta":{"content":" world"}}]}`},
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": true,
	}

	resp, respBody := h.doRequest("POST", "/v1/chat/completions", body)

	logID := resp.Header.Get("x-proxy-log-id")
	if logID == "" {
		t.Error("x-proxy-log-id header should be present in response")
	}

	// For streaming, response body should contain SSE data
	if !strings.Contains(respBody, "data:") {
		t.Error("streaming response should contain SSE data")
	}

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]
	h.verifyRecord(t, rec, "/v1/chat/completions", true, 200)
	h.verifyHeaders(t, rec)

	if len(rec.RequestBody) == 0 {
		t.Error("request_body should not be empty")
	}
	// For streaming, response body is reassembled SSE data
	if len(rec.ResponseBody) == 0 {
		t.Error("response_body should not be empty for streaming")
	}
	// Response body should be a JSON array string from ReassembleDataLines
	var respParsed interface{}
	if err := json.Unmarshal(rec.ResponseBody, &respParsed); err != nil {
		t.Errorf("streaming response_body should be valid JSON after reassembly: %v", err)
	}
}

func TestStreamingResponses(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType:  "sse",
		StatusCode:    200,
		SSEDataFields: []string{`{"id":"resp-1","output_text":"Hello"}`, `{"id":"resp-2","output_text":" world"}`},
	})

	body := map[string]interface{}{
		"model":  "gpt-4o",
		"input":  "Hello",
		"stream": true,
	}

	resp, _ := h.doRequest("POST", "/v1/responses", body)

	logID := resp.Header.Get("x-proxy-log-id")
	if logID == "" {
		t.Error("x-proxy-log-id header should be present in response")
	}

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]
	h.verifyRecord(t, rec, "/v1/responses", true, 200)
	h.verifyHeaders(t, rec)

	if len(rec.RequestBody) == 0 {
		t.Error("request_body should not be empty")
	}
	if len(rec.ResponseBody) == 0 {
		t.Error("response_body should not be empty for streaming")
	}
}

func TestAuthorizationHeaderPreserved(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	h.doRequest("POST", "/v1/chat/completions", body)

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]

	// Full raw logging - Authorization header must be preserved
	authHeader, ok := rec.RequestHeaders["Authorization"]
	if !ok {
		t.Fatal("Authorization header missing from request_headers")
	}
	if len(authHeader) == 0 || authHeader[0] != "Bearer test-api-key" {
		t.Errorf("Authorization header = %v, want [Bearer test-api-key]", authHeader)
	}
}

func TestResponseHeadersCaptured(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
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
		},
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	h.doRequest("POST", "/v1/chat/completions", body)

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]

	// Response headers should be captured
	if len(rec.ResponseHeaders) == 0 {
		t.Error("response_headers should not be empty")
	}

	// Content-Type should be present in response headers
	ct, ok := rec.ResponseHeaders["Content-Type"]
	if !ok {
		t.Error("Content-Type should be in response_headers")
	}
	if ok && len(ct) == 0 {
		t.Error("Content-Type header values should not be empty")
	}
}

func TestDurationGreaterThanZero(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		DelayMs:      50, // Add some delay so duration > 0
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	h.doRequest("POST", "/v1/chat/completions", body)

	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one JSONL record")
	}

	rec := records[0]
	if rec.DurationMs < 0 {
		t.Errorf("duration_ms should be >= 0, got %d", rec.DurationMs)
	}
}

func TestIntegration_RouteFiltering_AllowedRoute(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "gpt-4o",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "Hello from chat",
					},
					"finish_reason": "stop",
				},
			},
		},
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	switch resp, _ := h.doRequest("POST", "/v1/chat/completions", body); {
	case resp.StatusCode != 200:
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Wait for log delivery
	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	// Read and verify JSONL records
	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}

	// Should have exactly 1 record for allowed route
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 JSONL record, got %d", len(records))
	}

	rec := records[0]
	if rec.Route != "/v1/chat/completions" {
		t.Errorf("route = %s, want /v1/chat/completions", rec.Route)
	}
	if rec.UpstreamStatus != 200 {
		t.Errorf("upstream_status = %d, want 200", rec.UpstreamStatus)
	}
	if rec.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Errorf("terminal_status = %s, want %s", rec.TerminalStatus, logschema.TerminalStatusCompleted)
	}
}

func TestIntegration_RouteFiltering_ExcludedRoute(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"status": "ok",
			"data":   "admin dashboard",
		},
	})

	// Request to excluded /admin route - should still proxy successfully
	switch resp, _ := h.doRequest("GET", "/admin", nil); {
	case resp.StatusCode != 200:
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	// Wait for any potential log delivery
	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	// Read JSONL records - should have 0 records for excluded route
	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("expected 0 JSONL records for excluded /admin route, got %d", len(records))
	}
}

func TestIntegration_RouteFiltering_ErrorAllowedRoute(t *testing.T) {
	h := newTestHarness(t, testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   500,
		ResponseBody: map[string]interface{}{
			"error": map[string]interface{}{
				"message": "internal server error",
				"type":    "server_error",
			},
		},
	})

	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
		"stream": false,
	}

	switch resp, _ := h.doRequest("POST", "/v1/chat/completions", body); {
	case resp.StatusCode != 500:
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}

	// Wait for log delivery
	if err := h.waitForJSONL(); err != nil {
		t.Fatalf("waitForJSONL failed: %v", err)
	}

	// Read and verify JSONL records
	records, err := h.readJSONLRecords()
	if err != nil {
		t.Fatalf("readJSONLRecords failed: %v", err)
	}

	// Should have exactly 1 record for allowed route even with error
	if len(records) != 1 {
		t.Fatalf("expected exactly 1 JSONL record, got %d", len(records))
	}

	rec := records[0]
	if rec.Route != "/v1/chat/completions" {
		t.Errorf("route = %s, want /v1/chat/completions", rec.Route)
	}
	if rec.UpstreamStatus != 500 {
		t.Errorf("upstream_status = %d, want 500", rec.UpstreamStatus)
	}
	if rec.TerminalStatus != logschema.TerminalStatusUpstreamError {
		t.Errorf("terminal_status = %s, want %s", rec.TerminalStatus, logschema.TerminalStatusUpstreamError)
	}
}
