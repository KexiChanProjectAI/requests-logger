package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/user/openai-go-proxy-logger/internal/config"
	"github.com/user/openai-go-proxy-logger/internal/logclient"
	"github.com/user/openai-go-proxy-logger/internal/proxy"
)

func main() {
	cfg := config.LoadProxyConfig()

	logClient := logclient.NewClient(cfg)
	logClient.Start()
	defer logClient.Stop()

	// Set Gin to release mode to suppress debug warnings
	gin.SetMode(gin.ReleaseMode)
	// Create Gin engine with recovery middleware
	router := gin.New()
	router.Use(gin.Recovery())

	// Mount the proxy handler — all paths and methods go to upstream
	// Note: /*path also matches /, so no separate / route is needed.
	router.Any("/*path", gin.WrapH(proxy.NewHandler(cfg, logClient)))

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}

	log.Printf("Upstream: %s", cfg.UpstreamBaseURL)
	log.Printf("Log server: %s", cfg.LogServerURL)

	go func() {
		var err error
		if cfg.ProxyTLSCertFile != "" && cfg.ProxyTLSKeyFile != "" {
			log.Printf("Starting proxy HTTPS server on %s", cfg.ListenAddr)
			err = server.ListenAndServeTLS(cfg.ProxyTLSCertFile, cfg.ProxyTLSKeyFile)
		} else {
			log.Printf("Starting proxy HTTP server on %s", cfg.ListenAddr)
			err = server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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