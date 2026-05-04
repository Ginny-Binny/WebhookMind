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
	"github.com/gauravfs-14/webhookmind/internal/ingestion"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/gauravfs-14/webhookmind/internal/ratelimit"
	"github.com/gauravfs-14/webhookmind/internal/store"
	"github.com/redis/go-redis/v9"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("component", "ingestion")

	redisQueue, err := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisQueue.Close()

	pgStore, err := store.NewPostgresStore(context.Background(), cfg.Postgres.DSN)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pgStore.Close()

	pub, err := pubsub.NewPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Warn("failed to create pubsub publisher, SSE events disabled", "error", err)
	}
	if pub != nil {
		defer pub.Close()
	}

	// Build the rate limiter using a dedicated Redis client (kept distinct from the queue
	// connection so rate-limit checks don't fight queue traffic for connection pool slots).
	rlClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer rlClient.Close()
	limiter := ratelimit.NewLimiter(rlClient, cfg.Ingestion.RateLimitPerIP, cfg.Ingestion.RateLimitPerSource)
	logger.Info("rate limiter configured",
		"per_ip_per_minute", cfg.Ingestion.RateLimitPerIP,
		"per_source_per_minute", cfg.Ingestion.RateLimitPerSource,
	)

	handler := ingestion.NewHandler(
		redisQueue,
		pub,
		pgStore,
		limiter,
		logger,
		cfg.Ingestion.MaxBodyBytes,
		cfg.Ingestion.RequireSignature,
	)

	server := &http.Server{
		Addr:        fmt.Sprintf("0.0.0.0:%d", cfg.Ingestion.Port),
		Handler:     handler.Router(),
		ReadTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("ingestion server starting", "port", cfg.Ingestion.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server listen failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down ingestion server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("ingestion server stopped")
}
