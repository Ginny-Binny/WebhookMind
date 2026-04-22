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

	"github.com/gauravfs-14/webhookmind/internal/api"
	"github.com/gauravfs-14/webhookmind/internal/config"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/gauravfs-14/webhookmind/internal/replay"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("component", "api")

	pgStore, err := store.NewPostgresStore(context.Background(), cfg.Postgres.DSN)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pgStore.Close()

	// Initialize ScyllaDB for replay and webhook detail.
	scyllaStore, err := store.NewScyllaStore(cfg.Scylla.Hosts, cfg.Scylla.Keyspace)
	if err != nil {
		logger.Error("failed to connect to scylladb", "error", err)
		os.Exit(1)
	}
	defer scyllaStore.Close()

	// Initialize Redis pub/sub publisher (for metrics queries).
	pub, err := pubsub.NewPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Warn("failed to create pubsub publisher, metrics disabled", "error", err)
	}
	if pub != nil {
		defer pub.Close()
	}

	// Initialize Redis queue (for DLQ retry re-enqueue).
	redisQueue, err := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Warn("failed to connect to redis queue, DLQ retry disabled", "error", err)
	}
	if redisQueue != nil {
		defer redisQueue.Close()
	}

	replayEngine := replay.NewEngine(pgStore, scyllaStore, logger)
	srv := api.NewServer(pgStore, scyllaStore, pub, redisQueue, replayEngine, logger)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.API.Port),
		Handler:      srv.Router(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("api server starting", "port", cfg.API.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down api server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("api server shutdown failed", "error", err)
	}

	logger.Info("api server stopped")
}
