package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/gauravfs-14/webhookmind/internal/replay"
	"github.com/gauravfs-14/webhookmind/internal/store"
)

type Server struct {
	pg           *store.PostgresStore
	logger       *slog.Logger
	replayEngine *replay.Engine
}

func NewServer(pg *store.PostgresStore, replayEng *replay.Engine, logger *slog.Logger) *Server {
	return &Server{pg: pg, replayEngine: replayEng, logger: logger}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(jsonContentType)

	r.Get("/api/health", s.handleHealth)

	// Schema & Drift
	r.Get("/api/sources/{sourceID}/schema", s.handleGetSchema)
	r.Get("/api/sources/{sourceID}/drifts", s.handleGetDrifts)

	// Diffs
	r.Get("/api/sources/{sourceID}/diffs", s.handleGetDiffs)

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
