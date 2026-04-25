package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gauravfs-14/webhookmind/internal/config"
	"github.com/gauravfs-14/webhookmind/internal/extraction"
	"github.com/gauravfs-14/webhookmind/internal/filestore"
	"github.com/gauravfs-14/webhookmind/internal/models"
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
	})).With("component", "extractor-bridge")

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

	minioStore, err := filestore.NewMinIOStore(cfg.MinIO, logger)
	if err != nil {
		logger.Error("failed to connect to minio", "error", err)
		os.Exit(1)
	}

	extractor, err := newExtractor(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize extractor backend", "error", err)
		os.Exit(1)
	}
	defer extractor.Close()

	pub, err := pubsub.NewPublisher(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	if err != nil {
		logger.Warn("failed to create pubsub publisher, SSE events disabled", "error", err)
	}
	if pub != nil {
		defer pub.Close()
	}

	httpClient := &http.Client{
		Timeout: time.Duration(cfg.File.DownloadTimeoutSeconds) * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var (
		wg          sync.WaitGroup
		workerCount atomic.Int32
		maxWorkers  = int32(cfg.ExtractorBridge.MaxWorkers)
	)

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

				event, err := redisQueue.Dequeue(ctx, queue.QueueExtraction, 5*time.Second)
				if err != nil {
					logger.Error("dequeue failed", "worker_id", workerID, "error", err)
					continue
				}
				if event == nil {
					continue
				}

				processEvent(ctx, logger, event, cfg, httpClient, minioStore, extractor, pgStore, redisQueue, pub)
			}
		}(id)
	}

	for i := int32(0); i < int32(cfg.ExtractorBridge.Workers); i++ {
		startWorker(i)
	}

	logger.Info("extractor bridge starting",
		"workers", cfg.ExtractorBridge.Workers,
		"max_workers", cfg.ExtractorBridge.MaxWorkers,
	)

	// Scaler goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		nextWorkerID := int32(cfg.ExtractorBridge.Workers)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				depth, err := redisQueue.QueueLen(ctx, queue.QueueExtraction)
				if err != nil {
					logger.Error("failed to check queue depth", "error", err)
					continue
				}
				current := workerCount.Load()
				if depth > cfg.ExtractorBridge.QueueScaleThreshold && current < maxWorkers {
					toAdd := int32(10)
					if current+toAdd > maxWorkers {
						toAdd = maxWorkers - current
					}
					for i := int32(0); i < toAdd; i++ {
						startWorker(nextWorkerID)
						nextWorkerID++
					}
					logger.Info("scaled up workers", "added", toAdd, "total", workerCount.Load(), "queue_depth", depth)
				}
			}
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down extractor bridge, waiting for workers to finish")
	wg.Wait()
	logger.Info("extractor bridge stopped")
}

func processEvent(
	ctx context.Context,
	logger *slog.Logger,
	event *models.WebhookEvent,
	cfg *config.Config,
	httpClient *http.Client,
	minioStore *filestore.MinIOStore,
	extractor extraction.Extractor,
	pgStore *store.PostgresStore,
	redisQueue *queue.RedisQueue,
	pub *pubsub.Publisher,
) {
	startTime := time.Now()

	record := &models.ExtractionRecord{
		EventID:  event.ID,
		SourceID: event.SourceID,
		FileURL:  event.FileURL,
	}

	// 1. Download file from URL.
	fileData, err := downloadFile(ctx, httpClient, event.FileURL, cfg.File.MaxSizeBytes)
	if err != nil {
		logger.Error("file download failed",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"file_url", event.FileURL,
			"error", err,
		)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("download failed: %v", err)
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		// Still deliver original payload without extraction.
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	// 2. Detect file type.
	headerBytes := fileData
	if len(headerBytes) > 16 {
		headerBytes = headerBytes[:16]
	}
	fileType := extraction.DetectFileType(headerBytes)
	record.FileType = fileType

	if fileType == "unknown" {
		logger.Warn("unknown file type",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"file_url", event.FileURL,
		)
		record.Success = false
		record.ErrorMessage = "unknown file type"
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	// 3. Upload to MinIO.
	filename := extractFilename(event.FileURL, event.ID)
	now := time.Now().UTC()
	objectPath := fmt.Sprintf("%s/%d/%02d/%02d/%s/%s",
		event.SourceID, now.Year(), now.Month(), now.Day(), event.ID, filename)

	if err := minioStore.Upload(ctx, objectPath, bytes.NewReader(fileData), int64(len(fileData)), "application/octet-stream"); err != nil {
		logger.Error("minio upload failed",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error", err,
		)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("minio upload failed: %v", err)
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	record.MinIOPath = objectPath
	event.FileStorePath = objectPath

	// 4. Generate presigned URL for C++ engine (uses internal endpoint for Docker access).
	presignedURL, err := minioStore.GetInternalPresignedURL(ctx, objectPath, 5*time.Minute)
	if err != nil {
		logger.Error("presigned URL generation failed",
			"event_id", event.ID,
			"error", err,
		)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("presign failed: %v", err)
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	// 5. Call the extraction backend (local gRPC or cloud LLM).
	// Pass the already-downloaded file bytes so the cloud backend doesn't have to fetch from MinIO.
	resp, err := extractor.Extract(ctx, extraction.ExtractRequest{
		EventID:      event.ID,
		FilePath:     objectPath,
		FileType:     fileType,
		SourceID:     event.SourceID,
		PresignedURL: presignedURL,
		FileBytes:    fileData,
	})
	if err != nil {
		logger.Error("extraction backend failed",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error", err,
		)
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("grpc failed: %v", err)
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	if !resp.Success {
		logger.Error("extraction returned failure",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error_message", resp.ErrorMessage,
		)
		record.Success = false
		record.ErrorMessage = resp.ErrorMessage
		record.DurationMs = time.Since(startTime).Milliseconds()
		if insertErr := pgStore.InsertExtractionRecord(ctx, record); insertErr != nil {
			logger.Error("failed to record extraction", "event_id", event.ID, "error", insertErr)
		}
		enqueueForDelivery(ctx, logger, redisQueue, event)
		return
	}

	// 6. Parse extracted data and merge into payload.
	// LLMs often wrap JSON in markdown code fences (```json ... ```); strip those first.
	cleanedJSON := stripCodeFences(resp.ExtractedJSON)
	var extractedData map[string]any
	if err := json.Unmarshal([]byte(cleanedJSON), &extractedData); err != nil {
		logger.Error("failed to parse extracted json",
			"event_id", event.ID,
			"error", err,
		)
		extractedData = map[string]any{"raw": resp.ExtractedJSON}
	}

	event.ExtractedData = extractedData
	event.ExtractionMs = resp.DurationMs

	// Merge "extracted" key into RawBody.
	var payload map[string]any
	if err := json.Unmarshal(event.RawBody, &payload); err == nil {
		payload["extracted"] = extractedData
		if merged, err := json.Marshal(payload); err == nil {
			event.RawBody = merged
		}
	}

	// 7. Record extraction success.
	record.Success = true
	record.ExtractedData = extractedData
	record.TemplateID = resp.TemplateID
	record.CacheHit = resp.CacheHit
	record.DurationMs = time.Since(startTime).Milliseconds()
	if err := pgStore.InsertExtractionRecord(ctx, record); err != nil {
		logger.Error("failed to record extraction", "event_id", event.ID, "error", err)
	}

	// Upsert template if we got one.
	if resp.TemplateID != "" {
		if err := pgStore.UpsertTemplate(ctx, &models.Template{
			TemplateID:       resp.TemplateID,
			SourceID:         event.SourceID,
			FileType:         fileType,
			FieldPositionMap: extractedData,
			SampleEventID:    event.ID,
			ConfidenceScore:  1.0,
		}); err != nil {
			logger.Error("failed to upsert template", "event_id", event.ID, "error", err)
		}
	}

	logger.Info("extraction succeeded",
		"event_id", event.ID,
		"source_id", event.SourceID,
		"file_type", fileType,
		"template_id", resp.TemplateID,
		"cache_hit", resp.CacheHit,
		"duration_ms", resp.DurationMs,
	)

	if pub != nil {
		pub.Publish(ctx, pubsub.EventExtractionComplete, map[string]any{
			"event_id":    event.ID,
			"source_id":   event.SourceID,
			"template_id": resp.TemplateID,
			"cache_hit":   resp.CacheHit,
			"duration_ms": resp.DurationMs,
			"file_type":   fileType,
		})
	}

	// 8. Enqueue enriched event for delivery.
	enqueueForDelivery(ctx, logger, redisQueue, event)
}

func downloadFile(ctx context.Context, client *http.Client, fileURL string, maxSize int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, maxSize+1)
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("file exceeds max size %d bytes", maxSize)
	}

	return data, nil
}

func extractFilename(fileURL, eventID string) string {
	parsed, err := url.Parse(fileURL)
	if err != nil {
		return eventID
	}
	base := path.Base(parsed.Path)
	if base == "" || base == "." || base == "/" {
		return eventID
	}
	return base
}

// newExtractor picks an extractor backend based on config, optionally wrapped in a
// FallbackExtractor when EXTRACTOR_FALLBACK is set to a different backend.
// Supported backends: "local" (gRPC to the C++ extractor container), "cloud" (Anthropic API).
func newExtractor(cfg *config.Config, logger *slog.Logger) (extraction.Extractor, error) {
	primary, err := buildExtractorBackend(cfg.Extractor.Backend, cfg, logger, "primary")
	if err != nil {
		return nil, err
	}

	fallbackKind := strings.ToLower(cfg.Extractor.Fallback)
	if fallbackKind == "" || fallbackKind == "none" {
		return primary, nil
	}
	if fallbackKind == strings.ToLower(cfg.Extractor.Backend) {
		logger.Warn("EXTRACTOR_FALLBACK equals EXTRACTOR_BACKEND, ignoring fallback",
			"backend", cfg.Extractor.Backend,
		)
		return primary, nil
	}

	fallback, err := buildExtractorBackend(fallbackKind, cfg, logger, "fallback")
	if err != nil {
		// Don't kill startup just because the fallback couldn't init — log and continue with primary only.
		logger.Warn("failed to build extractor fallback, continuing with primary only",
			"fallback", fallbackKind,
			"error", err,
		)
		return primary, nil
	}

	logger.Info("extractor fallback configured",
		"primary", cfg.Extractor.Backend,
		"fallback", fallbackKind,
	)
	return extraction.NewFallbackExtractor(primary, fallback, logger), nil
}

func buildExtractorBackend(kind string, cfg *config.Config, logger *slog.Logger, role string) (extraction.Extractor, error) {
	switch strings.ToLower(kind) {
	case "cloud":
		logger.Info("initializing extractor backend",
			"role", role,
			"backend", "cloud",
			"model", cfg.Extractor.AnthropicModel,
		)
		return extraction.NewCloudExtractor(
			cfg.Extractor.AnthropicAPIKey,
			cfg.Extractor.AnthropicModel,
			cfg.Extractor.CloudTimeoutSeconds,
			logger,
		)
	case "local", "":
		logger.Info("initializing extractor backend", "role", role, "backend", "local")
		return extraction.NewLocalExtractor(cfg.Extractor.GRPCAddr, cfg.Extractor.GRPCTimeoutSeconds)
	default:
		return nil, fmt.Errorf("unknown extractor backend %q (expected 'local' or 'cloud')", kind)
	}
}

func enqueueForDelivery(ctx context.Context, logger *slog.Logger, redisQueue *queue.RedisQueue, event *models.WebhookEvent) {
	if err := redisQueue.Enqueue(ctx, queue.QueueDelivery, event); err != nil {
		logger.Error("failed to enqueue to delivery after extraction",
			"event_id", event.ID,
			"source_id", event.SourceID,
			"error", err,
		)
	}
}

// stripCodeFences removes markdown code fences that LLMs often wrap JSON in,
// returning the first fenced block's contents or the original string if no fence is present.
// Handles: "```json\n{...}\n```", "```\n{...}\n```", "{...}\n```\n<more text>", and plain JSON.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	// Case 1: already starts with JSON. Trim at the first trailing fence if present
	// (some LLMs emit valid JSON then more code blocks — we want only the first).
	if s[0] == '{' || s[0] == '[' {
		if idx := strings.Index(s, "```"); idx >= 0 {
			return strings.TrimSpace(s[:idx])
		}
		return s
	}

	// Case 2: starts with a fence. Skip past the opening ``` and optional language tag.
	start := strings.Index(s, "```")
	if start < 0 {
		return s
	}
	rest := s[start+3:]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest)
}
