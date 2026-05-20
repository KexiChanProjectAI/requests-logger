package logschema

import (
	"encoding/json"
	"time"
)

type TerminalStatus string

const (
	TerminalStatusCompleted          TerminalStatus = "completed"
	TerminalStatusClientDisconnected TerminalStatus = "client_disconnected"
	TerminalStatusUpstreamError      TerminalStatus = "upstream_error"
	TerminalStatusProxyError         TerminalStatus = "proxy_error"
)

type TruncationInfo struct {
	Truncated        bool `json:"truncated"`
	OriginalBytes    int  `json:"original_bytes"`
	CaptureMaxBytes  int  `json:"capture_max_bytes"`
}

type Record struct {
	LogID              string            `json:"log_id"`
	RequestID          string            `json:"request_id"`
	Route              string            `json:"route"`
	Method             string            `json:"method"`
	URL                string            `json:"url"`
	Query              string            `json:"query"`
	RequestTimestamp   time.Time         `json:"request_timestamp"`
	ResponseTimestamp  time.Time         `json:"response_timestamp"`
	DurationMs         int64             `json:"duration_ms"`
	TerminalStatus     TerminalStatus    `json:"terminal_status"`
	RequestHeaders     map[string][]string `json:"request_headers"`
	RequestBody        json.RawMessage   `json:"request_body"`
	ResponseHeaders    map[string][]string `json:"response_headers"`
	ResponseBody       json.RawMessage   `json:"response_body"`
	UpstreamStatus     int               `json:"upstream_status"`
	Error              string            `json:"error"`
	Stream             bool              `json:"stream"`
	TruncationInfo     *TruncationInfo   `json:"truncation_info,omitempty"`
}

func NewRecord() *Record {
	return &Record{
		RequestHeaders:  make(map[string][]string),
		ResponseHeaders: make(map[string][]string),
	}
}

func (r *Record) SetRequestHeaders(h map[string][]string) {
	if h == nil {
		r.RequestHeaders = make(map[string][]string)
		return
	}
	for k, v := range h {
		r.RequestHeaders[k] = v
	}
}

func (r *Record) SetResponseHeaders(h map[string][]string) {
	if h == nil {
		r.ResponseHeaders = make(map[string][]string)
		return
	}
	for k, v := range h {
		r.ResponseHeaders[k] = v
	}
}
