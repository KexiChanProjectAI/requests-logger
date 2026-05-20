package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
	"github.com/user/openai-go-proxy-logger/internal/sseutil"
)

type LogEnqueuer interface {
	Enqueue(record *logschema.Record)
}

type Handler struct {
	cfg         config.ProxyConfig
	logEnqueuer LogEnqueuer
	upstreamURL string
}

func NewHandler(cfg config.ProxyConfig, logEnqueuer LogEnqueuer) http.Handler {
	return &Handler{
		cfg:         cfg,
		logEnqueuer: logEnqueuer,
		upstreamURL: cfg.UpstreamBaseURL,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	reqBody, err := io.ReadAll(r.Body)
	if err != nil {
		h.enqueueError(r, startTime, "failed to read request body", logschema.TerminalStatusProxyError, 0, nil, nil)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	logID := uuid.New().String()
	requestID := r.Header.Get("x-request-id")
	isStream := requestIsStream(reqBody)

	upstreamURL := h.upstreamURL + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, bytes.NewReader(reqBody))
	if err != nil {
		h.enqueueError(r, startTime, "failed to create upstream request: "+err.Error(), logschema.TerminalStatusProxyError, 0, nil, nil)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	for k, values := range r.Header {
		for _, v := range values {
			upstreamReq.Header.Add(k, v)
		}
	}

	setForwardingHeaders(upstreamReq, r)

	client := &http.Client{}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		h.enqueueError(r, startTime, "upstream request failed: "+err.Error(), logschema.TerminalStatusUpstreamError, 0, reqBody, r.Header)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if isStream {
		h.handleStreamingResponse(w, r, resp, reqBody, logID, requestID, startTime)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.enqueueError(r, startTime, "failed to read response body: "+err.Error(), logschema.TerminalStatusUpstreamError, resp.StatusCode, reqBody, r.Header)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	terminalStatus := logschema.TerminalStatusCompleted
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		terminalStatus = logschema.TerminalStatusUpstreamError
	}

	record := logschema.NewRecord()
	record.LogID = logID
	record.RequestID = requestID
	record.Route = r.URL.Path
	record.Method = r.Method
	record.URL = upstreamURL
	record.Query = r.URL.RawQuery
	record.RequestTimestamp = startTime
	record.ResponseTimestamp = time.Now()
	record.DurationMs = record.ResponseTimestamp.Sub(record.RequestTimestamp).Milliseconds()
	record.TerminalStatus = terminalStatus
	record.SetRequestHeaders(r.Header)
	record.RequestBody = reqBody
	record.SetResponseHeaders(resp.Header)
	record.ResponseBody = sanitizeResponseBody(respBody)
	record.UpstreamStatus = resp.StatusCode
	record.Stream = false

	if h.cfg.CaptureMaxBytes > 0 {
		h.applyCaptureMax(record)
	}

	if h.logEnqueuer != nil {
		h.logEnqueuer.Enqueue(record)
	}

	w.Header().Set("x-proxy-log-id", logID)

	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	w.Write(respBody)
}

// sanitizeResponseBody ensures response body is stored as valid JSON.
// If the body is valid JSON, it returns it as-is wrapped in json.RawMessage.
// If not valid JSON (e.g., HTML error page), it wraps it in a JSON object.
func sanitizeResponseBody(body []byte) json.RawMessage {
	if json.Valid(body) {
		return json.RawMessage(body)
	}
	wrapped, _ := json.Marshal(map[string]string{
		"error": "non-JSON response",
		"raw":   string(body),
	})
	return json.RawMessage(wrapped)
}

func requestIsStream(reqBody []byte) bool {
	var body struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(reqBody, &body); err != nil {
		return false
	}
	return body.Stream
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, resp *http.Response, reqBody []byte, logID, requestID string, startTime time.Time) {
	w.Header().Set("x-proxy-log-id", logID)
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	terminalStatus := logschema.TerminalStatusCompleted
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		terminalStatus = logschema.TerminalStatusUpstreamError
	}

	tee := sseutil.NewTeeReader(resp.Body)
	buf := make([]byte, 32*1024)
outerLoop:
	for {
		n, readErr := tee.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				terminalStatus = logschema.TerminalStatusClientDisconnected
				break outerLoop
			}
			if flusher != nil {
				flusher.Flush()
			}
		}

		if readErr != nil {
			if readErr != io.EOF && terminalStatus == logschema.TerminalStatusCompleted {
				select {
				case <-r.Context().Done():
					terminalStatus = logschema.TerminalStatusClientDisconnected
					break outerLoop
				default:
					terminalStatus = logschema.TerminalStatusUpstreamError
				}
			}
			if readErr == io.EOF {
				terminalStatus = logschema.TerminalStatusCompleted
			}
			break outerLoop
		}

		select {
		case <-r.Context().Done():
			terminalStatus = logschema.TerminalStatusClientDisconnected
			break outerLoop
		default:
		}
		if terminalStatus == logschema.TerminalStatusClientDisconnected {
			break outerLoop
		}
	}

	captured := tee.Captured()
	if terminalStatus != logschema.TerminalStatusClientDisconnected && sseutil.HasDone(captured) {
		terminalStatus = logschema.TerminalStatusCompleted
	}

	upstreamURL := h.upstreamURL + r.URL.Path
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	dataLines := sseutil.DataLines(captured)
	var events []json.RawMessage
	for _, line := range dataLines {
		if line == "[DONE]" {
			continue
		}
		if json.Valid([]byte(line)) {
			events = append(events, json.RawMessage(line))
		}
	}

	var respBodyJSON json.RawMessage
	if len(events) > 0 {
		eventsJSON, _ := json.Marshal(events)
		respBodyJSON = eventsJSON
	} else {
		respBodyJSON = json.RawMessage(`[]`)
	}
	record := h.newRecord(r, logID, requestID, startTime, terminalStatus, resp.StatusCode, reqBody, resp.Header, respBodyJSON, true, upstreamURL)
	if h.cfg.CaptureMaxBytes > 0 {
		h.applyCaptureMax(record)
	}
	h.enqueueRecord(record)
}

func (h *Handler) newRecord(r *http.Request, logID, requestID string, startTime time.Time, terminalStatus logschema.TerminalStatus, upstreamStatus int, reqBody []byte, respHeaders http.Header, respBody []byte, stream bool, upstreamURL string) *logschema.Record {
	record := logschema.NewRecord()
	record.LogID = logID
	record.RequestID = requestID
	record.Route = r.URL.Path
	record.Method = r.Method
	record.URL = upstreamURL
	record.Query = r.URL.RawQuery
	record.RequestTimestamp = startTime
	record.ResponseTimestamp = time.Now()
	record.DurationMs = record.ResponseTimestamp.Sub(record.RequestTimestamp).Milliseconds()
	record.TerminalStatus = terminalStatus
	record.SetRequestHeaders(r.Header)
	record.RequestBody = reqBody
	record.SetResponseHeaders(respHeaders)
	record.ResponseBody = respBody
	record.UpstreamStatus = upstreamStatus
	record.Stream = stream
	return record
}

func (h *Handler) enqueueRecord(record *logschema.Record) {
	if h.logEnqueuer != nil {
		h.logEnqueuer.Enqueue(record)
	}
}

func (h *Handler) enqueueError(r *http.Request, startTime time.Time, errMsg string, status logschema.TerminalStatus, upstreamStatus int, reqBody []byte, reqHeaders http.Header) {
	requestID := ""
	if r != nil {
		requestID = r.Header.Get("x-request-id")
	}

	logID := uuid.New().String()

	record := logschema.NewRecord()
	record.LogID = logID
	record.RequestID = requestID
	if r != nil {
		record.Route = r.URL.Path
		record.Method = r.Method
		upstreamURL := h.upstreamURL + r.URL.Path
		if r.URL.RawQuery != "" {
			upstreamURL += "?" + r.URL.RawQuery
		}
		record.URL = upstreamURL
		record.Query = r.URL.RawQuery
	}
	record.RequestTimestamp = startTime
	record.ResponseTimestamp = time.Now()
	record.DurationMs = record.ResponseTimestamp.Sub(record.RequestTimestamp).Milliseconds()
	record.TerminalStatus = status
	if reqHeaders != nil {
		record.SetRequestHeaders(reqHeaders)
	}
	if reqBody != nil {
		record.RequestBody = reqBody
	}
	record.UpstreamStatus = upstreamStatus
	record.Error = errMsg
	record.Stream = false

	go func() {
		if h.logEnqueuer != nil {
			h.logEnqueuer.Enqueue(record)
		}
	}()

	log.Printf("proxy error: %s", errMsg)
}

func (h *Handler) applyCaptureMax(record *logschema.Record) {
	if record == nil {
		return
	}

	applyTruncation := func(raw json.RawMessage) (json.RawMessage, bool) {
		if len(raw) > h.cfg.CaptureMaxBytes {
			return raw[:h.cfg.CaptureMaxBytes], true
		}
		return raw, false
	}

	var truncated bool
	record.RequestBody, truncated = applyTruncation(record.RequestBody)
	if truncated {
		if record.TruncationInfo == nil {
			record.TruncationInfo = &logschema.TruncationInfo{}
		}
		record.TruncationInfo.Truncated = true
		record.TruncationInfo.OriginalBytes = len(record.RequestBody)
		record.TruncationInfo.CaptureMaxBytes = h.cfg.CaptureMaxBytes
	}

	record.ResponseBody, truncated = applyTruncation(record.ResponseBody)
	if truncated {
		if record.TruncationInfo == nil {
			record.TruncationInfo = &logschema.TruncationInfo{}
		}
		record.TruncationInfo.Truncated = true
	}
}

func isLocalhost(ip string) bool {
	if ip == "127.0.0.1" || ip == "::1" || ip == "localhost" {
		return true
	}
	if parsedIP := net.ParseIP(ip); parsedIP != nil {
		return parsedIP.IsLoopback()
	}
	return false
}

func setForwardingHeaders(outreq *http.Request, r *http.Request) {
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		clientIP = r.RemoteAddr
	}

	if isLocalhost(clientIP) {
		if existingXFF := r.Header.Get("X-Forwarded-For"); existingXFF != "" {
			outreq.Header.Set("X-Forwarded-For", existingXFF+", "+clientIP)
		} else {
			outreq.Header.Set("X-Forwarded-For", clientIP)
		}
	} else {
		outreq.Header.Set("X-Forwarded-For", clientIP)
	}

	outreq.Header.Set("X-Forwarded-Host", r.Host)

	proto := "http"
	if r.TLS != nil {
		proto = "https"
	} else if r.URL.Scheme == "https" {
		proto = "https"
	}
	outreq.Header.Set("X-Forwarded-Proto", proto)
}
