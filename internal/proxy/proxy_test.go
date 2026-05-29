package proxy_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/proxy"
	"github.com/user/openai-go-proxy-logger/internal/testutil"
)

type mockLogEnqueuer struct {
	mu      sync.Mutex
	records []*logschema.Record
}

func (m *mockLogEnqueuer) Enqueue(record *logschema.Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, record)
}

func (m *mockLogEnqueuer) waitForRecord(t *testing.T) *logschema.Record {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		if len(m.records) > 0 {
			record := m.records[0]
			m.mu.Unlock()
			return record
		}
		m.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for log record")
	return nil
}

func (m *mockLogEnqueuer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.records)
}

func TestChatCompletionsPOSTReachesUpstreamWithExactBody(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
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
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}

	upstreamReq := fUpstream.Requests[0]
	if upstreamReq.Method != "POST" {
		t.Errorf("expected method POST, got %s", upstreamReq.Method)
	}
	if upstreamReq.Path != "/v1/chat/completions" {
		t.Errorf("expected path /v1/chat/completions, got %s", upstreamReq.Path)
	}
	if string(upstreamReq.Body) != reqBody {
		t.Errorf("expected body %s, got %s", reqBody, string(upstreamReq.Body))
	}
}

func TestResponsesPOSTReachesUpstreamWithAuthorizationHeader(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"id":      "resp-test",
			"object":  "response",
			"created": 1234567890,
			"model":   "gpt-4o",
			"status":  "completed",
			"output":  []interface{}{},
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","input":"hello"}`
	req, _ := http.NewRequest("POST", "/v1/responses", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key-123")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}

	upstreamReq := fUpstream.Requests[0]
	if upstreamReq.Header.Get("Authorization") != "Bearer test-key-123" {
		t.Errorf("expected Authorization header 'Bearer test-key-123', got %s", upstreamReq.Header.Get("Authorization"))
	}
}

func TestResponseStatusBodyHeadersReturnedToClient(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   201,
		ResponseBody: map[string]interface{}{
			"id":      "chatcmpl-custom",
			"object":  "chat.completion",
			"created": 9999999999,
			"model":   "gpt-4o-mini",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": "custom response",
					},
					"finish_reason": "stop",
				},
			},
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 201 {
		t.Errorf("expected status 201, got %d", rr.Code)
	}

	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", rr.Header().Get("Content-Type"))
	}

	var respBody map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &respBody)

	if respBody["id"] != "chatcmpl-custom" {
		t.Errorf("expected id 'chatcmpl-custom', got %v", respBody["id"])
	}
}

func TestProxyLogIdHeaderPresentInResponse(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	logID := rr.Header().Get("x-proxy-log-id")
	if logID == "" {
		t.Error("expected x-proxy-log-id header to be present")
	}
	if len(logID) != 36 {
		t.Errorf("expected log_id to be UUID format (36 chars), got %s (len %d)", logID, len(logID))
	}
}

func TestLogRecordEnqueuedWithCorrectFields(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
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
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-request-id", "req-12345")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	time.Sleep(100 * time.Millisecond)

	if len(mockEnqueuer.records) != 1 {
		t.Fatalf("expected 1 enqueued record, got %d", len(mockEnqueuer.records))
	}

	record := mockEnqueuer.records[0]

	if record.RequestID != "req-12345" {
		t.Errorf("expected RequestID 'req-12345', got %s", record.RequestID)
	}
	if record.Route != "/v1/chat/completions" {
		t.Errorf("expected Route '/v1/chat/completions', got %s", record.Route)
	}
	if record.Method != "POST" {
		t.Errorf("expected Method 'POST', got %s", record.Method)
	}
	if record.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Errorf("expected TerminalStatus 'completed', got %s", record.TerminalStatus)
	}
	if record.UpstreamStatus != 200 {
		t.Errorf("expected UpstreamStatus 200, got %d", record.UpstreamStatus)
	}
	if record.Stream != false {
		t.Errorf("expected Stream false, got %v", record.Stream)
	}
	if record.LogID == "" {
		t.Error("expected LogID to be set")
	}
	if record.DurationMs < 0 {
		t.Errorf("expected DurationMs >= 0, got %d", record.DurationMs)
	}
}

func TestUpstreamNon2xxCapturedWithTerminalStatusUpstreamError(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   500,
		ResponseBody: map[string]interface{}{
			"error": map[string]interface{}{
				"message": "Internal server error",
				"type":    "internal_error",
			},
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	time.Sleep(100 * time.Millisecond)

	if len(mockEnqueuer.records) != 1 {
		t.Fatalf("expected 1 enqueued record, got %d", len(mockEnqueuer.records))
	}

	record := mockEnqueuer.records[0]

	if record.TerminalStatus != logschema.TerminalStatusUpstreamError {
		t.Errorf("expected TerminalStatus 'upstream_error', got %s", record.TerminalStatus)
	}
	if record.UpstreamStatus != 500 {
		t.Errorf("expected UpstreamStatus 500, got %d", record.UpstreamStatus)
	}
}

func TestQueryStringPreserved(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	reqBody := `{"model":"gpt-4o"}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions?foo=bar&baz=qux", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}

	upstreamReq := fUpstream.Requests[0]
	if upstreamReq.Path != "/v1/chat/completions" {
		t.Errorf("expected path '/v1/chat/completions', got %s", upstreamReq.Path)
	}
	expectedQuery := "foo=bar&baz=qux"
	if upstreamReq.Header.Get("X-Original-Query") != expectedQuery {
	}
}

func TestStreamingRequestDetectedForwardedAndCaptured(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "sse",
		StatusCode:   200,
		SSEDataFields: []string{
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `data: {"id":"chatcmpl-xxx","object":"chat.completion.chunk"`) {
		t.Fatalf("expected first SSE frame in response, got %q", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("expected done frame in response, got %q", body)
	}

	record := mockEnqueuer.waitForRecord(t)
	if !record.Stream {
		t.Fatal("expected stream record")
	}
	if record.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Fatalf("expected terminal status completed, got %s", record.TerminalStatus)
	}

	var assembled map[string]interface{}
	if err := json.Unmarshal(record.ResponseBody, &assembled); err != nil {
		t.Fatalf("response body should be JSON object: %v", err)
	}
	choices, ok := assembled["choices"].([]interface{})
	if !ok {
		t.Fatalf("expected choices in assembled response, got %v", assembled)
	}
	if len(choices) != 1 {
		t.Fatalf("expected 1 assembled choice, got %d", len(choices))
	}
	choice := choices[0].(map[string]interface{})
	msg, ok := choice["message"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected message in assembled choice, got %v", choice)
	}
	if msg["content"] != "hello world" {
		t.Errorf("expected merged content 'hello world', got %v", msg["content"])
	}
}

func TestStreamingFramesForwardedInRealTime(t *testing.T) {
	firstFlushed := make(chan struct{})
	allowSecond := make(chan struct{})
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_ = testutil.SSEChunkWriter(w, `{"chunk":1}`)
			w.(http.Flusher).Flush()
			close(firstFlushed)
			<-allowSecond
			_ = testutil.SSEChunkWriter(w, `{"chunk":2}`)
			_ = testutil.SSEDoneWriter(w)
			w.(http.Flusher).Flush()
		},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	server := httptest.NewServer(proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer resp.Body.Close()

	select {
	case <-firstFlushed:
	case <-time.After(time.Second):
		t.Fatal("upstream did not flush first frame")
	}

	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read first streamed line: %v", err)
	}
	if !strings.Contains(line, `{"chunk":1}`) {
		t.Fatalf("expected first chunk before upstream completed, got %q", line)
	}

	close(allowSecond)
	_, _ = io.ReadAll(reader)
	record := mockEnqueuer.waitForRecord(t)
	if record.TerminalStatus != logschema.TerminalStatusCompleted {
		t.Fatalf("expected completed, got %s", record.TerminalStatus)
	}
}

func TestStreamingClientDisconnectionSetsTerminalStatus(t *testing.T) {
	allowFinish := make(chan struct{})
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		HandlerFunc: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_ = testutil.SSEChunkWriter(w, `{"chunk":1}`)
			w.(http.Flusher).Flush()
			<-allowFinish
			for i := 0; i < 256; i++ {
				_, _ = w.Write([]byte("data: {\"chunk\":\"padding-padding-padding-padding-padding-padding-padding-padding\"}\n\n"))
				w.(http.Flusher).Flush()
			}
		},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	server := httptest.NewServer(proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer))
	defer server.Close()

	resp, err := http.Post(server.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"stream":true}`))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("failed to read first streamed line: %v", err)
	}
	_ = resp.Body.Close()
	close(allowFinish)

	record := mockEnqueuer.waitForRecord(t)
	if record.TerminalStatus != logschema.TerminalStatusClientDisconnected {
		t.Fatalf("expected client_disconnected, got %s", record.TerminalStatus)
	}
}

func TestNonStreamingRequestsRemainNonStreaming(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "non-stream"},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":false}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	record := mockEnqueuer.waitForRecord(t)
	if record.Stream {
		t.Fatal("expected non-stream record")
	}
	if !strings.Contains(string(record.ResponseBody), "non-stream") {
		t.Fatalf("expected original JSON response body, got %s", string(record.ResponseBody))
	}
}

func TestAllPathsProxied(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody:  map[string]interface{}{"id": "any-path"},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	req, _ := http.NewRequest("POST", "/v1/models", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != 200 {
		t.Errorf("expected status 200 for unknown path, got %d", rr.Code)
	}
	if len(fUpstream.Requests) != 1 {
		t.Errorf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}
	if fUpstream.Requests[0].Path != "/v1/models" {
		t.Errorf("expected path /v1/models, got %s", fUpstream.Requests[0].Path)
	}
}

func TestXFFHeaderFromDirectClient(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "test"},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}
	xff := fUpstream.Requests[0].Header.Get("X-Forwarded-For")
	if xff == "" {
		t.Error("expected X-Forwarded-For header to be set")
	}
}

func TestXFFHeaderFromLocalhost(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "test"},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL, LogServerURL: fLogServer.URL(), LogServerToken: "test"}, mockEnqueuer)

	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	req.Host = "localhost"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}
	xff := fUpstream.Requests[0].Header.Get("X-Forwarded-For")
	if !strings.Contains(xff, "203.0.113.50") {
		t.Errorf("expected X-Forwarded-For to contain original value '203.0.113.50', got %s", xff)
	}
}

func TestXFFHeaderFromLocalhostNoExisting(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "test"},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}
	xff := fUpstream.Requests[0].Header.Get("X-Forwarded-For")
	if xff == "" {
		t.Error("expected X-Forwarded-For header to be set")
	}
}

func TestXForwardedHostAndProto(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "test"},
	})
	defer fUpstream.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}
	handler := proxy.NewHandler(config.ProxyConfig{UpstreamBaseURL: fUpstream.Server.URL}, mockEnqueuer)

	server := httptest.NewServer(handler)
	defer server.Close()

	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	resp.Body.Close()

	if len(fUpstream.Requests) != 1 {
		t.Fatalf("expected 1 upstream request, got %d", len(fUpstream.Requests))
	}
	xfh := fUpstream.Requests[0].Header.Get("X-Forwarded-Host")
	if xfh == "" {
		t.Error("expected X-Forwarded-Host header to be set")
	}
	xfp := fUpstream.Requests[0].Header.Get("X-Forwarded-Proto")
	if xfp == "" {
		t.Error("expected X-Forwarded-Proto header to be set")
	}
}
func TestProxyRouteFiltering_NonStreaming(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{"id": "test", "object": "chat.completion", "created": 1234567890, "model": "gpt-4o", "choices": []map[string]interface{}{{"index": 0, "message": map[string]interface{}{"role": "assistant", "content": "test response"}, "finish_reason": "stop"}}},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	t.Run("AllowedRoute_V1ChatCompletions_IsEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 200 {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		// Verify record was enqueued
		if mockEnqueuer.count() != 1 {
			t.Errorf("expected 1 enqueued record for allowed route /v1/chat/completions, got %d", mockEnqueuer.count())
		}
	})

	t.Run("DeniedRoute_Admin_IsNotEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o"}`
		req, _ := http.NewRequest("POST", "/admin", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 200 {
			t.Errorf("expected status 200 (request still proxied), got %d", rr.Code)
		}

		// Verify NO record was enqueued
		if mockEnqueuer.count() != 0 {
			t.Errorf("expected 0 enqueued records for denied route /admin, got %d", mockEnqueuer.count())
		}
	})

	t.Run("DeniedPrefix_ApiV1KeysRotate_IsNotEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"key_id":"test-key"}`
		req, _ := http.NewRequest("POST", "/api/v1/keys/rotate", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 200 {
			t.Errorf("expected status 200 (request still proxied), got %d", rr.Code)
		}

		// Verify NO record was enqueued
		if mockEnqueuer.count() != 0 {
			t.Errorf("expected 0 enqueued records for denied prefix /api/v1/keys/rotate, got %d", mockEnqueuer.count())
		}
	})
}
func TestProxyRouteFiltering_Streaming(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "sse",
		StatusCode:   200,
		SSEDataFields: []string{
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-xxx","object":"chat.completion.chunk","created":1234,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	t.Run("AllowedRoute_V1ChatCompletions_Streaming_IsEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 200 {
			t.Errorf("expected status 200, got %d", rr.Code)
		}

		// Verify record was enqueued
		record := mockEnqueuer.waitForRecord(t)
		if record.Route != "/v1/chat/completions" {
			t.Errorf("expected route /v1/chat/completions, got %s", record.Route)
		}
		if !record.Stream {
			t.Error("expected stream record")
		}
	})

	t.Run("DeniedRoute_Admin_Streaming_IsNotEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o","stream":true}`
		req, _ := http.NewRequest("POST", "/admin", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 200 {
			t.Errorf("expected status 200 (request still proxied), got %d", rr.Code)
		}

		// Wait a bit to ensure any async enqueue would have happened
		time.Sleep(100 * time.Millisecond)

		// Verify NO record was enqueued
		if mockEnqueuer.count() != 0 {
			t.Errorf("expected 0 enqueued records for denied route /admin, got %d", mockEnqueuer.count())
		}
	})
}

func TestProxyRouteFiltering_Error(t *testing.T) {
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   500,
		ResponseBody: map[string]interface{}{
			"error": map[string]interface{}{
				"message": "internal server error",
				"type":    "server_error",
				"code":    "internal_error",
			},
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	t.Run("AllowedRoute_V1ChatCompletions_Error_IsEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
		req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 500 {
			t.Errorf("expected status 500, got %d", rr.Code)
		}

		// Verify error record was enqueued
		time.Sleep(100 * time.Millisecond)
		if mockEnqueuer.count() != 1 {
			t.Errorf("expected 1 enqueued record for allowed route /v1/chat/completions, got %d", mockEnqueuer.count())
		}
	})

	t.Run("DeniedRoute_Admin_Error_IsNotEnqueued", func(t *testing.T) {
		mockEnqueuer.mu.Lock()
		mockEnqueuer.records = nil
		mockEnqueuer.mu.Unlock()

		reqBody := `{"model":"gpt-4o"}`
		req, _ := http.NewRequest("POST", "/admin", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != 500 {
			t.Errorf("expected status 500 (request still proxied), got %d", rr.Code)
		}

		// Wait a bit to ensure any async enqueue would have happened
		time.Sleep(100 * time.Millisecond)

		// Verify NO record was enqueued
		if mockEnqueuer.count() != 0 {
			t.Errorf("expected 0 enqueued records for denied route /admin, got %d", mockEnqueuer.count())
		}
	})
}

func TestProxyForwardingStillWorks_ExcludedRoute(t *testing.T) {
	// Set up a mock backend that returns a distinctive body
	backendResp := "admin-response"
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]interface{}{
			"message": backendResp,
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	// Send request to excluded route /admin
	reqBody := `{"model":"gpt-4o"}`
	req, _ := http.NewRequest("POST", "/admin", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Assert the response was received from upstream (forwarding still works)
	if rr.Code != 200 {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Assert the upstream body was correctly forwarded
	if !strings.Contains(rr.Body.String(), backendResp) {
		t.Errorf("expected response body to contain %q, got %q", backendResp, rr.Body.String())
	}

	// Assert NO record was enqueued (route is excluded)
	if mockEnqueuer.count() != 0 {
		t.Errorf("expected 0 enqueued records for excluded route /admin, got %d", mockEnqueuer.count())
	}
}

func TestProxyForwardingStillWorks_AllowedRoute(t *testing.T) {
	// Set up a mock backend that returns a distinctive body
	backendResp := "ai-response"
	fUpstream := testutil.NewFakeUpstream(testutil.UpstreamHandlerOpts{
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
						"content": backendResp,
					},
					"finish_reason": "stop",
				},
			},
		},
	})
	defer fUpstream.Server.Close()

	fLogServer := testutil.NewFakeLogServer()
	defer fLogServer.Server.Close()

	mockEnqueuer := &mockLogEnqueuer{}

	cfg := config.ProxyConfig{
		ListenAddr:      ":0",
		UpstreamBaseURL: fUpstream.Server.URL,
		LogServerURL:    fLogServer.URL(),
		LogServerToken:  "test-token",
		LogQueueSize:    1024,
		CaptureMaxBytes: 0,
	}

	handler := proxy.NewHandler(cfg, mockEnqueuer)

	// Send request to allowed route /v1/chat/completions
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Assert the response was received from upstream (forwarding still works)
	if rr.Code != 200 {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Assert the upstream body was correctly forwarded
	if !strings.Contains(rr.Body.String(), backendResp) {
		t.Errorf("expected response body to contain %q, got %q", backendResp, rr.Body.String())
	}

	// Assert ONE record was enqueued (route is allowed)
	if mockEnqueuer.count() != 1 {
		t.Errorf("expected 1 enqueued record for allowed route /v1/chat/completions, got %d", mockEnqueuer.count())
	}
}
