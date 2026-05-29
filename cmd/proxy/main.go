package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

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
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	log.Printf("Starting proxy server on %s", cfg.ListenAddr)
	log.Printf("Upstream: %s", cfg.UpstreamBaseURL)
	log.Printf("Log server: %s", cfg.LogServerURL)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	var profileServer *http.Server
	if cfg.ProfileEnabled {
		profileServer = &http.Server{
			Addr:    cfg.ProfileListenAddr,
			Handler: http.DefaultServeMux,
		}
		go func() {
			log.Printf("Starting profiling server on %s", cfg.ProfileListenAddr)
			if err := profileServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("Profiling server error: %v", err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down proxy server...")

	if profileServer != nil {
		if err := profileServer.Shutdown(context.Background()); err != nil {
			log.Printf("Profiling server shutdown error: %v", err)
		}
	}

	if err := server.Shutdown(context.Background()); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	log.Println("Proxy server stopped")
}