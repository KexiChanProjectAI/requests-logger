package sseutil_test

import (
	"encoding/json"
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

func TestAssembleFinalStateEmpty(t *testing.T) {
	result := sseutil.AssembleFinalState([]byte{})
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected null, got %v", out)
	}
}

func TestAssembleFinalStateTruncatedSSE(t *testing.T) {
	truncated := []byte("data: {\"incomplete")
	result := sseutil.AssembleFinalState(truncated)
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("expected valid JSON from truncated input, got error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected null for truncated SSE, got %v", out)
	}
}

func TestAssembleFinalStateOnlyDONE(t *testing.T) {
	sse := []byte("data: [DONE]\n\n")
	result := sseutil.AssembleFinalState(sse)
	var out interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected null for DONE-only SSE, got %v", out)
	}
}

func TestAssembleChatCompletionsFinalState(t *testing.T) {
	sse := []byte(
		"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"created\":1234,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"created\":1234,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hello\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"created\":1234,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n" +
			"data: {\"id\":\"chatcmpl-xxx\",\"object\":\"chat.completion.chunk\",\"created\":1234,\"model\":\"gpt-4o\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\n" +
			"data: [DONE]\n\n")

	result := sseutil.AssembleFinalState(sse)
	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}

	if out["id"] != "chatcmpl-xxx" {
		t.Errorf("expected id chatcmpl-xxx, got %v", out["id"])
	}
	if out["object"] != "chat.completion" {
		t.Errorf("expected object chat.completion, got %v", out["object"])
	}
	if out["created"] != float64(1234) {
		t.Errorf("expected created 1234, got %v", out["created"])
	}
	if out["model"] != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %v", out["model"])
	}

	choices, ok := out["choices"].([]interface{})
	if !ok || len(choices) != 1 {
		t.Fatalf("expected 1 choice, got %v", choices)
	}
	choice := choices[0].(map[string]interface{})
	msg := choice["message"].(map[string]interface{})
	if msg["role"] != "assistant" {
		t.Errorf("expected role assistant, got %v", msg["role"])
	}
	if msg["content"] != "Hello world" {
		t.Errorf("expected content 'Hello world', got %v", msg["content"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("expected finish_reason stop, got %v", choice["finish_reason"])
	}

	usage, ok := out["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage object, got %v", out["usage"])
	}
	if usage["prompt_tokens"].(float64) != 10 {
		t.Errorf("expected prompt_tokens 10, got %v", usage["prompt_tokens"])
	}
}

func TestAssembleResponsesAPIFinalState(t *testing.T) {
	sse := []byte(
		"event: response.created\n" +
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_xxx\",\"object\":\"response\",\"status\":\"in_progress\"}}\n\n" +
			"event: response.output_item.added\n" +
			"data: {\"type\":\"response.output_item.added\",\"response\":{\"id\":\"resp_xxx\"}}\n\n" +
			"event: response.completed\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_xxx\",\"object\":\"response\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"Final response\"}]}],\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"total_tokens\":8}}}\n\n" +
			"data: [DONE]\n\n")

	result := sseutil.AssembleFinalState(sse)
	var out map[string]interface{}
	if err := json.Unmarshal(result, &out); err != nil {
		t.Fatalf("expected valid JSON, got error: %v", err)
	}

	if out["id"] != "resp_xxx" {
		t.Errorf("expected id resp_xxx, got %v", out["id"])
	}
	if out["status"] != "completed" {
		t.Errorf("expected status completed, got %v", out["status"])
	}
	usage, ok := out["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage object, got %v", out["usage"])
	}
	if usage["total_tokens"].(float64) != 8 {
		t.Errorf("expected total_tokens 8, got %v", usage["total_tokens"])
	}
}
