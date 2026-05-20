package logserver_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/logserver"
)

type mockRecordWriter struct {
	records []*logschema.Record
	err     error
}

func (m *mockRecordWriter) Write(record *logschema.Record) error {
	m.records = append(m.records, record)
	return m.err
}

func TestNewHandler(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "test-token",
	}
	writer := &mockRecordWriter{}
	h := logserver.NewHandler(cfg, writer)
	if h == nil {
		t.Fatal("NewHandler returned nil")
	}
}

func TestHandlePostLogs_ValidToken(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	record := logschema.NewRecord()
	record.LogID = "log-123"
	record.RequestID = "req-456"
	record.Route = "/v1/chat/completions"
	record.Method = "POST"
	record.URL = "https://api.openai.com/v1/chat/completions"
	record.TerminalStatus = logschema.TerminalStatusCompleted
	record.RequestHeaders = map[string][]string{
		"Authorization": {"Bearer sk-xxx"},
		"Content-Type":  {"application/json"},
	}
	record.ResponseHeaders = map[string][]string{
		"Content-Type": {"application/json"},
	}

	body, _ := json.Marshal(record)
	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	if len(writer.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(writer.records))
	}

	got := writer.records[0]
	if got.LogID != "log-123" {
		t.Errorf("expected LogID 'log-123', got '%s'", got.LogID)
	}
	if got.RequestID != "req-456" {
		t.Errorf("expected RequestID 'req-456', got '%s'", got.RequestID)
	}
	if got.Route != "/v1/chat/completions" {
		t.Errorf("expected Route '/v1/chat/completions', got '%s'", got.Route)
	}
}

func TestHandlePostLogs_InvalidToken(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	record := logschema.NewRecord()
	body, _ := json.Marshal(record)
	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer wrong-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}

	if len(writer.records) != 0 {
		t.Errorf("expected 0 records written, got %d", len(writer.records))
	}
}

func TestHandlePostLogs_MissingAuth(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	record := logschema.NewRecord()
	body, _ := json.Marshal(record)
	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}

	if len(writer.records) != 0 {
		t.Errorf("expected 0 records written, got %d", len(writer.records))
	}
}

func TestHandlePostLogs_InvalidJSON(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", "Bearer valid-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}

	if len(writer.records) != 0 {
		t.Errorf("expected 0 records written, got %d", len(writer.records))
	}
}

func TestHandlePostLogs_NonPOSTMethod(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	tCases := []struct {
		method string
	}{
		{http.MethodGet},
		{http.MethodPut},
		{http.MethodDelete},
		{http.MethodPatch},
		{http.MethodHead},
		{http.MethodOptions},
	}

	for _, tc := range tCases {
		writer.records = nil
		req := httptest.NewRequest(tc.method, "/logs", nil)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected status %d, got %d", tc.method, http.StatusMethodNotAllowed, rr.Code)
		}

		if len(writer.records) != 0 {
			t.Errorf("%s: expected 0 records written, got %d", tc.method, len(writer.records))
		}
	}
}

func TestHandlePostLogs_NoRedaction(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{}
	handler := logserver.NewHandler(cfg, writer)

	record := logschema.NewRecord()
	record.LogID = "log-no-redact"
	record.RequestHeaders = map[string][]string{
		"Authorization": {"Bearer sk-secret-key-12345"},
		"X-API-Key":    {"my-api-key"},
	}
	record.ResponseHeaders = map[string][]string{
		"Content-Type": {"application/json"},
	}
	record.RequestBody = json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`)
	record.ResponseBody = json.RawMessage(`{"id":"chatcmpl-123","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hi"}}]}`)

	body, _ := json.Marshal(record)
	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, rr.Code)
	}

	if len(writer.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(writer.records))
	}

	got := writer.records[0]

	authHeader := got.RequestHeaders["Authorization"]
	if len(authHeader) != 1 || authHeader[0] != "Bearer sk-secret-key-12345" {
		t.Errorf("Authorization header was redacted or modified, got %v", authHeader)
	}

	xAPIKey := got.RequestHeaders["X-API-Key"]
	if len(xAPIKey) != 1 || xAPIKey[0] != "my-api-key" {
		t.Errorf("X-API-Key header was redacted or modified, got %v", xAPIKey)
	}

	if string(got.RequestBody) != `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}` {
		t.Errorf("RequestBody was modified, got %s", string(got.RequestBody))
	}

	if string(got.ResponseBody) != `{"id":"chatcmpl-123","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"Hi"}}]}` {
		t.Errorf("ResponseBody was modified, got %s", string(got.ResponseBody))
	}
}

func TestHandlePostLogs_WriterError(t *testing.T) {
	cfg := config.LogServerConfig{
		LogServerToken: "valid-token",
	}
	writer := &mockRecordWriter{err: errors.New("write failed")}
	handler := logserver.NewHandler(cfg, writer)

	record := logschema.NewRecord()
	body, _ := json.Marshal(record)
	req := httptest.NewRequest(http.MethodPost, "/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}
}
