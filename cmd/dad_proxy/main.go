package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dad_proxy/internal/config"
	"dad_proxy/internal/handlers"
	"dad_proxy/internal/logger"
	"dad_proxy/internal/share"
	"dad_proxy/internal/version"
)

func main() {
	// Config
	cfg, err := config.Load()
	if err != nil {
		panic("Failed to load config: " + err.Error())
	}

	// Logger
	log := logger.NewLogger(cfg.Environment)

	log.Info("Starting DAD Proxy Server",
		"app", "dad_proxy",
		"version", version.AppVersion,
		"DAD_PROXY_API_PORT", cfg.ProxyPort,
		"DAD_PROXY_API_HELLO", cfg.APIHelloPath,
		"DAD_API_URL", cfg.APIURL,
		"DAD_PROXY_IP", cfg.ProxyIP,
		"DAD_PROXY_SHARE", cfg.ProxyShare,
		"DAD_PROXY_PORTS_RANGE_START", cfg.PortsRangeStart,
		"DAD_PROXY_PORTS_RANGE_END", cfg.PortsRangeEnd,
		"DAD_PROXY_TCP_PAYLOAD_REWRITE", cfg.TCPPayloadRewrite,
		"DAD_PROXY_UDP_IDLE_TIMEOUT", cfg.UDPIdleTimeout,
	)

	// Share proxy
	if cfg.ProxyShare == true {
		shareClient := share.NewProxyShare(cfg, log)
		shareClient.SendConfigAsync()
	}

	// Handler
	proxyHandler := handlers.NewProxyHandler(cfg, log)

	// HTTP API Server
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tunnels", proxyHandler.HandleTunnels)
	mux.HandleFunc("/", proxyHandler.HandleProxy)

	server := &http.Server{
		Addr:         ":" + cfg.ProxyPort,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("Server listening", "port", cfg.ProxyPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Shutting down dad_proxy server...")
	proxyHandler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown", "error", err)
	}

	log.Info("dad_proxy server exited")
}
