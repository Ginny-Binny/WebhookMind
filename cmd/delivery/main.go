package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gauravfs-14/webhookmind/internal/config"
	"github.com/gauravfs-14/webhookmind/internal/delivery"
	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
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
	})).With("component", "delivery")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Initialize Redis.
	redisQueue, err := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisQueue.Close()

	// Initialize PostgreSQL.
	pg, err := store.NewPostgresStore(ctx, cfg.Postgres.DSN)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pg.Close()

	// Initialize ScyllaDB.
	scylla, err := store.NewScyllaStore(cfg.Scylla.Hosts, cfg.Scylla.Keyspace)
	if err != nil {
		logger.Error("failed to connect to scylladb", "error", err)
		os.Exit(1)
	}
	defer scylla.Close()

	// Initialize pub/sub publisher for SSE events.
	pub, err := pubsub.NewPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Warn("failed to create pubsub publisher, SSE events disabled", "error", err)
	}
	if pub != nil {
		defer pub.Close()
	}

	// Create delivery engine.
	engine := delivery.NewEngine(redisQueue, pg, scylla, pub, logger, cfg.Delivery.MaxRetries, cfg.Schema.MinSamples)

	// Recover incomplete deliveries from a previous crash.
	engine.RecoverIncomplete(ctx)

	// Start worker pool.
	logger.Info("delivery engine starting",
		"workers", cfg.Delivery.Workers,
		"max_retries", cfg.Delivery.MaxRetries,
	)
	wg := engine.Run(ctx, cfg.Delivery.Workers)

	<-ctx.Done()
	logger.Info("shutting down delivery engine, waiting for in-flight deliveries")
	wg.Wait()
	logger.Info("delivery engine stopped")
}
