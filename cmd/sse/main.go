package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/config"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/sse"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("component", "sse")

	// Create Redis subscriber.
	subscriber, err := pubsub.NewSubscriber(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Error("failed to connect to redis for pub/sub", "error", err)
		os.Exit(1)
	}
	defer subscriber.Close()

	// Create SSE hub.
	hub := sse.NewHub(logger)

	// Start Redis subscriber in background — feeds into hub.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("subscribing to Redis pub/sub channel", "channel", pubsub.Channel)
		if err := subscriber.Subscribe(ctx, func(msg string) {
			hub.Broadcast(msg)
		}); err != nil {
			logger.Error("redis subscriber error", "error", err)
		}
	}()

	// Start HTTP server.
	handler := sse.NewHandler(hub, logger)

	mux := http.NewServeMux()
	mux.Handle("/events", handler)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"ok","clients":%d}`, hub.ClientCount())
	})

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.SSE.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE connections are long-lived
	}

	go func() {
		logger.Info("SSE server starting", "port", cfg.SSE.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("SSE server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down SSE server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("SSE server shutdown failed", "error", err)
	}

	logger.Info("SSE server stopped")
}
