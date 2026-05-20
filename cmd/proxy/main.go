package main

import (
	"log"
	"net/http"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logclient"
	"github.com/user/openai-go-proxy-logger/internal/proxy"
)

func main() {
	cfg := config.LoadProxyConfig()

	logClient := logclient.NewClient(cfg)
	logClient.Start()
	defer logClient.Stop()

	handler := proxy.NewHandler(cfg, logClient)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: handler,
	}

	log.Printf("Starting proxy server on %s", cfg.ListenAddr)
	log.Printf("Upstream: %s", cfg.UpstreamBaseURL)
	log.Printf("Log server: %s", cfg.LogServerURL)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}
