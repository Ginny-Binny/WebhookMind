package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/config"
	"github.com/gauravfs-14/webhookmind/internal/queue"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	})).With("component", "orchestrator")

	redisQueue, err := queue.NewRedisQueue(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisQueue.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		wg          sync.WaitGroup
		workerCount atomic.Int32
		maxWorkers  = int32(cfg.Orchestrator.MaxWorkers)
	)

	// startWorker launches a single worker goroutine.
	startWorker := func(id int32) {
		wg.Add(1)
		workerCount.Add(1)
		go func(workerID int32) {
			defer wg.Done()
			defer workerCount.Add(-1)
			logger.Debug("worker started", "worker_id", workerID)

			for {
				select {
				case <-ctx.Done():
					logger.Debug("worker stopping", "worker_id", workerID)
					return
				default:
				}

				event, err := redisQueue.Dequeue(ctx, queue.QueueIncoming, 5*time.Second)
				if err != nil {
					logger.Error("dequeue failed",
						"worker_id", workerID,
						"error", err,
					)
					continue
				}

				if event == nil {
					// Timeout, no event available. Loop and try again.
					continue
				}

				// Detect file_url in payload to route to extraction queue.
				targetQueue := queue.QueueDelivery
				var payload map[string]interface{}
				if err := json.Unmarshal(event.RawBody, &payload); err == nil {
					if fileURL, ok := payload["file_url"].(string); ok && fileURL != "" {
						event.FileURL = fileURL
						targetQueue = queue.QueueExtraction
					}
				}

				if err := redisQueue.Enqueue(ctx, targetQueue, event); err != nil {
					logger.Error("failed to enqueue event",
						"worker_id", workerID,
						"event_id", event.ID,
						"source_id", event.SourceID,
						"target_queue", targetQueue,
						"error", err,
					)
					continue
				}

				if targetQueue == queue.QueueExtraction {
					logger.Debug("event routed to extraction",
						"worker_id", workerID,
						"event_id", event.ID,
						"source_id", event.SourceID,
						"file_url", event.FileURL,
					)
				} else {
					logger.Debug("event routed to delivery",
						"worker_id", workerID,
						"event_id", event.ID,
						"source_id", event.SourceID,
					)
				}
			}
		}(id)
	}

	// Start initial workers.
	for i := int32(0); i < int32(cfg.Orchestrator.Workers); i++ {
		startWorker(i)
	}

	logger.Info("orchestrator started",
		"initial_workers", cfg.Orchestrator.Workers,
		"max_workers", cfg.Orchestrator.MaxWorkers,
		"scale_threshold", cfg.Orchestrator.QueueScaleThreshold,
	)

	// Scaler goroutine: check queue depth every 10 seconds.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()

		nextWorkerID := int32(cfg.Orchestrator.Workers)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				depth, err := redisQueue.QueueLen(ctx, queue.QueueIncoming)
				if err != nil {
					logger.Error("failed to check queue depth", "error", err)
					continue
				}

				current := workerCount.Load()
				if depth > cfg.Orchestrator.QueueScaleThreshold && current < maxWorkers {
					// Scale up: add workers to handle the backlog.
					toAdd := int32(10) // Add 10 workers at a time.
					if current+toAdd > maxWorkers {
						toAdd = maxWorkers - current
					}

					for i := int32(0); i < toAdd; i++ {
						startWorker(nextWorkerID)
						nextWorkerID++
					}

					logger.Info("scaled up workers",
						"added", toAdd,
						"total", workerCount.Load(),
						"queue_depth", depth,
					)
				}
			}
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down orchestrator, waiting for workers to finish")
	wg.Wait()
	logger.Info("orchestrator stopped")
}
