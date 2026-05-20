package main

import (
	"log"
	"net/http"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/jsonl"
	"github.com/user/openai-go-proxy-logger/internal/logserver"
)

func main() {
	cfg := config.LoadLogServerConfig()

	writer := jsonl.NewWriter(cfg)
	defer writer.Close()

	handler := logserver.NewHandler(cfg, writer)

	log.Printf("Log server starting on %s", cfg.ListenAddr)
	log.Printf("Log directory: %s", cfg.LogDir)
	if err := http.ListenAndServe(cfg.ListenAddr, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
