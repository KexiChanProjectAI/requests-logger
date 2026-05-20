package logserver

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logschema"
)

// RecordWriter is the interface for writing log records.
type RecordWriter interface {
	Write(record *logschema.Record) error
}

type handler struct {
	cfg    config.LogServerConfig
	writer RecordWriter
}

func NewHandler(cfg config.LogServerConfig, writer RecordWriter) http.Handler {
	h := &handler{
		cfg:    cfg,
		writer: writer,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /logs", h.handlePostLogs)
	return mux
}

func (h *handler) handlePostLogs(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
		return
	}

	const bearerPrefix = "Bearer "
	if len(authHeader) <= len(bearerPrefix) {
		http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
		return
	}

	token := authHeader[len(bearerPrefix):]
	if token != h.cfg.LogServerToken {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	if r.Body == nil {
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	var record logschema.Record
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if err := h.writer.Write(&record); err != nil {
		log.Printf("Failed to write record: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte("Accepted"))
}
