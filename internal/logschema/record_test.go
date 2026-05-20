package logschema

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRecordJSONMarshaling(t *testing.T) {
	reqTimestamp := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	respTimestamp := time.Date(2024, 1, 15, 10, 30, 1, 500000000, time.UTC)

	truncInfo := &TruncationInfo{
		Truncated:       true,
		OriginalBytes:   5000,
		CaptureMaxBytes: 1024,
	}

	record := &Record{
		LogID:             "log-123",
		RequestID:         "req-456",
		Route:             "/v1/chat/completions",
		Method:            "POST",
		URL:               "https://api.openai.com/v1/chat/completions",
		Query:             "",
		RequestTimestamp:  reqTimestamp,
		ResponseTimestamp: respTimestamp,
		DurationMs:        1500,
		TerminalStatus:    TerminalStatusCompleted,
		RequestHeaders: map[string][]string{
			"Authorization": {"Bearer sk-xxx"},
			"Content-Type":  {"application/json"},
		},
		RequestBody:     json.RawMessage(`{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`),
		ResponseHeaders: map[string][]string{
			"Content-Type": {"application/json"},
		},
		ResponseBody:   json.RawMessage(`{"id":"chatcmpl-xxx","object":"chat.completion","model":"gpt-4"}`),
		UpstreamStatus: 200,
		Error:          "",
		Stream:         false,
		TruncationInfo: truncInfo,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var unmarshaled Record
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if unmarshaled.LogID != record.LogID {
		t.Errorf("LogID mismatch: got %q, want %q", unmarshaled.LogID, record.LogID)
	}
	if unmarshaled.RequestID != record.RequestID {
		t.Errorf("RequestID mismatch: got %q, want %q", unmarshaled.RequestID, record.RequestID)
	}
	if unmarshaled.Route != record.Route {
		t.Errorf("Route mismatch: got %q, want %q", unmarshaled.Route, record.Route)
	}
	if unmarshaled.Method != record.Method {
		t.Errorf("Method mismatch: got %q, want %q", unmarshaled.Method, record.Method)
	}
	if unmarshaled.DurationMs != record.DurationMs {
		t.Errorf("DurationMs mismatch: got %d, want %d", unmarshaled.DurationMs, record.DurationMs)
	}
	if unmarshaled.TerminalStatus != record.TerminalStatus {
		t.Errorf("TerminalStatus mismatch: got %q, want %q", unmarshaled.TerminalStatus, record.TerminalStatus)
	}
	if unmarshaled.UpstreamStatus != record.UpstreamStatus {
		t.Errorf("UpstreamStatus mismatch: got %d, want %d", unmarshaled.UpstreamStatus, record.UpstreamStatus)
	}
	if unmarshaled.Stream != record.Stream {
		t.Errorf("Stream mismatch: got %v, want %v", unmarshaled.Stream, record.Stream)
	}

	if unmarshaled.TruncationInfo == nil {
		t.Fatal("TruncationInfo should not be nil")
	}
	if unmarshaled.TruncationInfo.Truncated != truncInfo.Truncated {
		t.Errorf("TruncationInfo.Truncated mismatch")
	}
}

func TestRecordKeepsAuthorization(t *testing.T) {
	record := &Record{
		LogID:          "log-auth-test",
		RequestID:      "req-auth-789",
		RequestHeaders: map[string][]string{
			"Authorization": {"Bearer sk-secret-key-12345"},
			"Content-Type":  {"application/json"},
		},
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)

	if !strings.Contains(jsonStr, "Bearer sk-secret-key-12345") {
		t.Errorf("Authorization header value was not preserved in JSON output: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "Authorization") {
		t.Errorf("Authorization header key was not preserved in JSON output: %s", jsonStr)
	}
}

func TestTerminalStatusConstants(t *testing.T) {
	if TerminalStatusCompleted != "completed" {
		t.Errorf("TerminalStatusCompleted = %q, want %q", TerminalStatusCompleted, "completed")
	}
	if TerminalStatusClientDisconnected != "client_disconnected" {
		t.Errorf("TerminalStatusClientDisconnected = %q, want %q", TerminalStatusClientDisconnected, "client_disconnected")
	}
	if TerminalStatusUpstreamError != "upstream_error" {
		t.Errorf("TerminalStatusUpstreamError = %q, want %q", TerminalStatusUpstreamError, "upstream_error")
	}
	if TerminalStatusProxyError != "proxy_error" {
		t.Errorf("TerminalStatusProxyError = %q, want %q", TerminalStatusProxyError, "proxy_error")
	}
}

func TestNewRecord(t *testing.T) {
	record := NewRecord()
	if record.RequestHeaders == nil {
		t.Error("NewRecord should initialize RequestHeaders")
	}
	if record.ResponseHeaders == nil {
		t.Error("NewRecord should initialize ResponseHeaders")
	}
}

func TestSetRequestHeaders(t *testing.T) {
	record := NewRecord()
	h := map[string][]string{
		"Authorization": {"Bearer token"},
		"X-Request-ID":  {"req-123"},
	}

	record.SetRequestHeaders(h)

	if record.RequestHeaders == nil {
		t.Fatal("RequestHeaders is nil after SetRequestHeaders")
	}
	if len(record.RequestHeaders) != 2 {
		t.Fatalf("expected 2 headers, got %d: %v", len(record.RequestHeaders), record.RequestHeaders)
	}
	val, ok := record.RequestHeaders["Authorization"]
	if !ok || len(val) != 1 || val[0] != "Bearer token" {
		t.Errorf("Authorization header not set correctly: %v", val)
	}
	val, ok = record.RequestHeaders["X-Request-ID"]
	if !ok || len(val) != 1 || val[0] != "req-123" {
		t.Errorf("X-Request-ID header not set correctly: %v", val)
	}
}

func TestSetRequestHeadersNil(t *testing.T) {
	record := NewRecord()
	record.SetRequestHeaders(nil)

	if record.RequestHeaders == nil {
		t.Error("SetRequestHeaders(nil) should initialize map, not leave it nil")
	}
	if len(record.RequestHeaders) != 0 {
		t.Errorf("SetRequestHeaders(nil) should produce empty map, got %d entries", len(record.RequestHeaders))
	}
}

func TestSetResponseHeaders(t *testing.T) {
	record := NewRecord()
	h := map[string][]string{
		"Content-Type":   {"application/json"},
		"X-Response-ID": {"resp-456"},
	}

	record.SetResponseHeaders(h)

	if record.ResponseHeaders == nil {
		t.Fatal("ResponseHeaders is nil after SetResponseHeaders")
	}
	if len(record.ResponseHeaders) != 2 {
		t.Fatalf("expected 2 headers, got %d: %v", len(record.ResponseHeaders), record.ResponseHeaders)
	}
	val, ok := record.ResponseHeaders["Content-Type"]
	if !ok || len(val) != 1 || val[0] != "application/json" {
		t.Errorf("Content-Type header not set correctly: %v", val)
	}
	val, ok = record.ResponseHeaders["X-Response-ID"]
	if !ok || len(val) != 1 || val[0] != "resp-456" {
		t.Errorf("X-Response-ID header not set correctly: %v", val)
	}
}

func TestSetResponseHeadersNil(t *testing.T) {
	record := NewRecord()
	record.SetResponseHeaders(nil)

	if record.ResponseHeaders == nil {
		t.Error("SetResponseHeaders(nil) should initialize map, not leave it nil")
	}
	if len(record.ResponseHeaders) != 0 {
		t.Errorf("SetResponseHeaders(nil) should produce empty map, got %d entries", len(record.ResponseHeaders))
	}
}

func TestTruncationInfo(t *testing.T) {
	ti := TruncationInfo{
		Truncated:       true,
		OriginalBytes:   10000,
		CaptureMaxBytes: 1024,
	}

	data, err := json.Marshal(ti)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var unmarshaled TruncationInfo
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if unmarshaled.Truncated != true {
		t.Errorf("Truncated mismatch: got %v, want true", unmarshaled.Truncated)
	}
	if unmarshaled.OriginalBytes != 10000 {
		t.Errorf("OriginalBytes mismatch: got %d, want 10000", unmarshaled.OriginalBytes)
	}
	if unmarshaled.CaptureMaxBytes != 1024 {
		t.Errorf("CaptureMaxBytes mismatch: got %d, want 1024", unmarshaled.CaptureMaxBytes)
	}
}

func TestRecordBodyRawMessage(t *testing.T) {
	record := &Record{
		LogID:       "log-body-test",
		RequestBody: json.RawMessage(`{"key":"value with special chars <>&\""}`),
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var unmarshaled Record
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if len(unmarshaled.RequestBody) == 0 {
		t.Error("RequestBody should not be empty after round-trip")
	}

	if string(unmarshaled.RequestBody) == "" {
		t.Error("RequestBody should preserve content after JSON round-trip")
	}
}

func TestSanitizeRawMessages(t *testing.T) {
	record := &Record{
		LogID: "test-sanitize",
	}

	record.RequestBody = nil
	record.ResponseBody = nil
	record.SanitizeRawMessages()

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal after SanitizeRawMessages failed: %v", err)
	}

	var unmarshaled Record
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if string(unmarshaled.RequestBody) != "null" {
		t.Errorf("RequestBody should be null, got %q", string(unmarshaled.RequestBody))
	}
	if string(unmarshaled.ResponseBody) != "null" {
		t.Errorf("ResponseBody should be null, got %q", string(unmarshaled.ResponseBody))
	}
}

func TestSanitizeRawMessagesPreservesValidJSON(t *testing.T) {
	record := &Record{
		LogID:       "test-sanitize-valid",
		RequestBody: json.RawMessage(`{"model":"gpt-4"}`),
	}

	record.SanitizeRawMessages()

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var unmarshaled Record
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if string(unmarshaled.RequestBody) != `{"model":"gpt-4"}` {
		t.Errorf("RequestBody should be preserved, got %q", string(unmarshaled.RequestBody))
	}
}

func TestSanitizeRawMessagesHandlesInvalidJSON(t *testing.T) {
	record := &Record{
		LogID:       "test-sanitize-invalid",
		RequestBody: json.RawMessage(`{invalid`),
	}

	record.SanitizeRawMessages()

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal should not fail for invalid RawMessage: %v", err)
	}

	var unmarshaled Record
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if string(unmarshaled.RequestBody) != "null" {
		t.Errorf("RequestBody with invalid JSON should become null, got %q", string(unmarshaled.RequestBody))
	}
}
