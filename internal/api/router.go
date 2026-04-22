package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/gauravfs-14/webhookmind/internal/pubsub"
	"github.com/gauravfs-14/webhookmind/internal/queue"
	"github.com/gauravfs-14/webhookmind/internal/replay"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

type Server struct {
	pg           *store.PostgresStore
	scylla       *store.ScyllaStore
	pub          *pubsub.Publisher
	queue        *queue.RedisQueue
	logger       *slog.Logger
	replayEngine *replay.Engine
}

func NewServer(pg *store.PostgresStore, scylla *store.ScyllaStore, pub *pubsub.Publisher, q *queue.RedisQueue, replayEng *replay.Engine, logger *slog.Logger) *Server {
	return &Server{pg: pg, scylla: scylla, pub: pub, queue: q, replayEngine: replayEng, logger: logger}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(corsMiddleware)
	r.Use(jsonContentType)

	r.Get("/api/health", s.handleHealth)

	// Sources & Webhooks
	r.Get("/api/sources", s.handleListSources)
	r.Get("/api/sources/{sourceID}/webhooks", s.handleListWebhooks)
	r.Get("/api/webhooks/{eventID}", s.handleGetWebhookDetail)

	// Schema & Drift
	r.Get("/api/sources/{sourceID}/schema", s.handleGetSchema)
	r.Get("/api/sources/{sourceID}/drifts", s.handleGetDrifts)

	// Diffs
	r.Get("/api/sources/{sourceID}/diffs", s.handleGetDiffs)

	// DLQ
	r.Get("/api/dlq", s.handleListDLQ)
	r.Post("/api/dlq/{eventID}/retry", s.handleRetryDLQ)
	r.Post("/api/dlq/{eventID}/discard", s.handleDiscardDLQ)

	// Metrics
	r.Get("/api/metrics/throughput", s.handleThroughputMetrics)
	r.Get("/api/metrics/latency", s.handleLatencyMetrics)

	// Routing Rules
	r.Post("/api/sources/{sourceID}/rules", s.handleCreateRule)
	r.Get("/api/sources/{sourceID}/rules", s.handleListRules)
	r.Put("/api/rules/{ruleID}", s.handleUpdateRule)
	r.Delete("/api/rules/{ruleID}", s.handleDeleteRule)
	r.Post("/api/rules/{ruleID}/test", s.handleTestRule)

	// Replay
	r.Post("/api/sources/{sourceID}/replay", s.handleStartReplay)
	r.Get("/api/replay/{sessionID}", s.handleGetReplay)
	r.Post("/api/replay/{sessionID}/pause", s.handlePauseReplay)
	r.Post("/api/replay/{sessionID}/resume", s.handleResumeReplay)
	r.Delete("/api/replay/{sessionID}", s.handleCancelReplay)

	return r
}

func jsonContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
