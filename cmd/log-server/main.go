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
	"github.com/user/openai-go-proxy-logger/internal/jsonl"
	"github.com/user/openai-go-proxy-logger/internal/logserver"
)

func main() {
	cfg := config.LoadLogServerConfig()

	writer := jsonl.NewWriter(cfg)
	defer writer.Close()

	handler := logserver.NewHandler(cfg, writer)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	log.Printf("Log server starting on %s", cfg.ListenAddr)
	log.Printf("Log directory: %s", cfg.LogDir)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
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

	log.Println("Shutting down log server...")

	if profileServer != nil {
		if err := profileServer.Shutdown(context.Background()); err != nil {
			log.Printf("Profiling server shutdown error: %v", err)
		}
	}

	if err := server.Shutdown(context.Background()); err != nil {
		log.Fatalf("Server shutdown error: %v", err)
	}

	log.Println("Log server stopped")
}