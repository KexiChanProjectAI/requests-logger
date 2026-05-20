package sseutil

import (
	"bytes"
	"encoding/json"
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

// AssembleFinalState reassembles streaming SSE data into the final complete response object.
// For the Responses API: extracts the response from the response.completed event.
// For Chat Completions: merges all delta.content into a single response object.
// Always returns valid JSON - returns null for empty/invalid input.
func AssembleFinalState(sseData []byte) json.RawMessage {
	defer func() {
		if r := recover(); r != nil {
			// Recover from any panics during assembly
		}
	}()

	// Handle empty input
	if len(sseData) == 0 {
		return json.RawMessage(`null`)
	}

	events, err := parseSSEEvents(sseData)
	if err != nil && len(events) == 0 {
		return json.RawMessage(`null`)
	}
	if len(events) == 0 {
		return json.RawMessage(`null`)
	}

	var result json.RawMessage

	// Check if this is a Responses API stream (has event types)
	for _, ev := range events {
		if ev.Type == "response.completed" {
			result = extractResponsesAPIFinalState(ev.Data)
			break
		}
	}

	// If no response.completed found, try chat completions assembly
	if result == nil || len(result) == 0 {
		result = assembleChatCompletionsFromEvents(events)
	}

	// Validate the result - if invalid, return null
	if len(result) == 0 || !json.Valid(result) {
		return json.RawMessage(`null`)
	}

	return result
}

// parseSSEEvents parses SSE data and returns events with their Type preserved.
func parseSSEEvents(sseData []byte) ([]sse.Event, error) {
	var events []sse.Event

	readEvents := sse.Read(bytes.NewReader(sseData), nil)
	readEvents(func(event sse.Event, err error) bool {
		if err != nil {
			return false
		}
		events = append(events, event)
		return true
	})

	return events, nil
}

// extractResponsesAPIFinalState extracts the response object from a response.completed event.
// The data payload looks like: {"type":"response.completed","response":{...full response...}}
// Returns null on any error to ensure valid JSON output.
func extractResponsesAPIFinalState(data string) json.RawMessage {
	if data == "" {
		return json.RawMessage(`null`)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return json.RawMessage(`null`)
	}
	if resp, ok := payload["response"]; ok {
		result, err := json.Marshal(resp)
		if err != nil {
			return json.RawMessage(`null`)
		}
		return json.RawMessage(result)
	}
	result, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(result)
}

// assembleChatCompletionsFromEvents merges chat completion chunks into a single response.
// Returns null on error to ensure valid JSON output.
func assembleChatCompletionsFromEvents(events []sse.Event) json.RawMessage {
	// Collect all JSON data payloads (skip [DONE])
	var chunks []map[string]interface{}
	for _, ev := range events {
		data := strings.TrimSpace(ev.Data)
		if data == "" || data == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil {
			chunks = append(chunks, chunk)
		}
	}

	if len(chunks) == 0 {
		return json.RawMessage(`null`)
	}

	// Check if this is actually a chat completions stream
	isChatChunk := false
	for _, c := range chunks {
		if obj, ok := c["object"].(string); ok && obj == "chat.completion.chunk" {
			isChatChunk = true
			break
		}
	}

	if !isChatChunk {
		// Unknown format — return last event as final state
		last, err := json.Marshal(chunks[len(chunks)-1])
		if err != nil {
			return json.RawMessage(`null`)
		}
		return json.RawMessage(last)
	}

	// Assemble chat completions
	result := make(map[string]interface{})

	// Copy base fields from first chunk
	first := chunks[0]
	result["id"] = first["id"]
	result["object"] = "chat.completion"
	result["created"] = first["created"]
	result["model"] = first["model"]

	// Merge choices by index
	choiceMap := make(map[int]*mergedChoice)
	for _, chunk := range chunks {
		choices, ok := chunk["choices"].([]interface{})
		if !ok {
			continue
		}
		for _, c := range choices {
			choice, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			idx := int(choice["index"].(float64))

			if _, exists := choiceMap[idx]; !exists {
				choiceMap[idx] = &mergedChoice{
					Index:   idx,
					Message: make(map[string]interface{}),
				}
			}
			mc := choiceMap[idx]

			// Merge delta into message
			if delta, ok := choice["delta"].(map[string]interface{}); ok {
				if role, ok := delta["role"].(string); ok {
					mc.Message["role"] = role
				}
				if content, ok := delta["content"].(string); ok {
					if existing, ok := mc.Message["content"].(string); ok {
						mc.Message["content"] = existing + content
					} else {
						mc.Message["content"] = content
					}
				}
				if toolCalls, ok := delta["tool_calls"].([]interface{}); ok {
					mc.Message["tool_calls"] = toolCalls
				}
			}

			if fr, ok := choice["finish_reason"]; ok && fr != nil {
				mc.FinishReason = fr
			}
		}
	}

	// Build choices array
	choices := make([]map[string]interface{}, 0, len(choiceMap))
	for i := 0; i < len(choiceMap); i++ {
		if mc, ok := choiceMap[i]; ok {
			c := map[string]interface{}{
				"index":         mc.Index,
				"message":       mc.Message,
				"finish_reason": mc.FinishReason,
			}
			choices = append(choices, c)
		}
	}
	result["choices"] = choices

	// Take usage from last chunk that has it
	for i := len(chunks) - 1; i >= 0; i-- {
		if usage, ok := chunks[i]["usage"]; ok && usage != nil {
			result["usage"] = usage
			break
		}
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return json.RawMessage(jsonBytes)
}

type mergedChoice struct {
	Index        int
	Message      map[string]interface{}
	FinishReason interface{}
}
