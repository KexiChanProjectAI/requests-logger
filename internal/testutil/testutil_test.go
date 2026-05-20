package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestFakeUpstreamJSON(t *testing.T) {
	fu := NewFakeUpstream(UpstreamHandlerOpts{
		ResponseType: "json",
		StatusCode:   200,
		ResponseBody: map[string]string{"result": "ok"},
	})
	defer fu.Server.Close()

	resp, err := http.Get(fu.Server.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Errorf("expected Content-Type application/json, got %s", got)
	}

	if len(fu.Requests) != 1 {
		t.Errorf("expected 1 request, got %d", len(fu.Requests))
	}
	if fu.Requests[0].Path != "/v1/chat/completions" {
		t.Errorf("expected path /v1/chat/completions, got %s", fu.Requests[0].Path)
	}
}

func TestFakeUpstreamSSE(t *testing.T) {
	fu := NewFakeUpstream(UpstreamHandlerOpts{
		ResponseType:  "sse",
		StatusCode:    200,
		SSEDataFields: []string{`{"content":"hello"}`, `{"content":"world"}`},
	})
	defer fu.Server.Close()

	resp, err := http.Get(fu.Server.URL + "/v1/chat/completions")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Errorf("expected Content-Type text/event-stream, got %s", got)
	}

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	body := buf.String()

	if !strings.Contains(body, "data: {\"content\":\"hello\"}\n\n") {
		t.Errorf("missing SSE chunk in body: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]\n\n") {
		t.Errorf("missing [DONE] in body: %s", body)
	}
}

func TestFakeUpstreamRecordsRequest(t *testing.T) {
	fu := NewFakeUpstream(UpstreamHandlerOpts{ResponseType: "json"})
	defer fu.Server.Close()

	req, _ := http.NewRequest("POST", fu.Server.URL+"/v1/responses", strings.NewReader(`{"model":"gpt-4"}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(req)

	if len(fu.Requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(fu.Requests))
	}
	if fu.Requests[0].Method != "POST" {
		t.Errorf("expected POST, got %s", fu.Requests[0].Method)
	}
	if fu.Requests[0].Path != "/v1/responses" {
		t.Errorf("expected /v1/responses, got %s", fu.Requests[0].Path)
	}
	if fu.Requests[0].Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("expected Authorization header, got %s", fu.Requests[0].Header.Get("Authorization"))
	}
}

func TestFakeLogServer(t *testing.T) {
	fls := NewFakeLogServer()
	defer fls.Server.Close()

	req, _ := http.NewRequest("POST", fls.URL()+"/logs", strings.NewReader(`{"event":"test"}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("expected status 202, got %d", resp.StatusCode)
	}

	captures := fls.GetCaptures()
	if len(captures) != 1 {
		t.Errorf("expected 1 capture, got %d", len(captures))
	}

	if !bytes.Contains(captures[0].Body, []byte(`"event":"test"`)) {
		t.Errorf("expected body to contain event, got %s", captures[0].Body)
	}

	if captures[0].Header.Get("Authorization") != "Bearer secret-token" {
		t.Errorf("expected Authorization header, got %s", captures[0].Header.Get("Authorization"))
	}
}

func TestFakeLogServerCaptureCount(t *testing.T) {
	fls := NewFakeLogServer()
	defer fls.Server.Close()

	if fls.CaptureCount() != 0 {
		t.Errorf("expected 0 captures initially, got %d", fls.CaptureCount())
	}

	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest("POST", fls.URL()+"/logs", strings.NewReader(`{}`))
		http.DefaultClient.Do(req)
	}

	if fls.CaptureCount() != 3 {
		t.Errorf("expected 3 captures, got %d", fls.CaptureCount())
	}
}

func TestSSEChunkWriter(t *testing.T) {
	buf := new(bytes.Buffer)
	data := `{"content":"hello"}`

	err := SSEChunkWriter(buf, data)
	if err != nil {
		t.Fatalf("SSEChunkWriter failed: %v", err)
	}

	got := buf.String()
	want := "data: {\"content\":\"hello\"}\n\n"
	if got != want {
		t.Errorf("SSEChunkWriter produced %q, want %q", got, want)
	}
}

func TestSSEDoneWriter(t *testing.T) {
	buf := new(bytes.Buffer)

	err := SSEDoneWriter(buf)
	if err != nil {
		t.Fatalf("SSEDoneWriter failed: %v", err)
	}

	got := buf.String()
	want := "data: [DONE]\n\n"
	if got != want {
		t.Errorf("SSEDoneWriter produced %q, want %q", got, want)
	}
}

func TestJSONResponseWriter(t *testing.T) {
	buf := new(bytes.Buffer)
	w := &testResponseWriter{buf: buf, header: make(http.Header)}

	v := map[string]string{"result": "ok"}
	err := JSONResponseWriter(w, http.StatusOK, v)
	if err != nil {
		t.Fatalf("JSONResponseWriter failed: %v", err)
	}

	if w.statusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.statusCode)
	}

	if ct := w.header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]string
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("expected result 'ok', got %s", result["result"])
	}
}

type testResponseWriter struct {
	buf       *bytes.Buffer
	header    http.Header
	statusCode int
}

func (w *testResponseWriter) Header() http.Header {
	return w.header
}

func (w *testResponseWriter) Write(b []byte) (int, error) {
	return w.buf.Write(b)
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func TestNewTempJSONLDir(t *testing.T) {
	dir := NewTempJSONLDir(t)

	if dir == "" {
		t.Fatal("expected non-empty dir path")
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("temp dir should exist: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory, got file")
	}
}

func TestReadJSONLLines(t *testing.T) {
	dir := NewTempJSONLDir(t)

	jsonlPath := "test.jsonl"
	jsonlContent := `{"id":"1","event":"start"}
{"id":"2","event":"end"}
`
	if err := os.WriteFile(dir+"/"+jsonlPath, []byte(jsonlContent), 0644); err != nil {
		t.Fatalf("failed to write test JSONL: %v", err)
	}

	records := ReadJSONLLines(t, dir, jsonlPath)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	if records[0]["id"] != "1" {
		t.Errorf("expected id '1', got %v", records[0]["id"])
	}
	if records[1]["event"] != "end" {
		t.Errorf("expected event 'end', got %v", records[1]["event"])
	}
}
