package main

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"
)

//go:embed web/*
var webFS embed.FS

type AdminServer struct {
	cfg          *Config
	tokenMgr     *TokenManager
	logger       *RequestLogger
	authResolver *AuthResolver
	startAt      time.Time
}

func NewAdminServer(cfg *Config, tokenMgr *TokenManager, logger *RequestLogger, authResolver *AuthResolver) *AdminServer {
	return &AdminServer{
		cfg:          cfg,
		tokenMgr:     tokenMgr,
		logger:       logger,
		authResolver: authResolver,
		startAt:      time.Now(),
	}
}

func (s *AdminServer) Start(addr string) {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/overview", s.handleOverview)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/config/model", s.handleConfigModel)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/logs/errors", s.handleErrors)
	mux.HandleFunc("/api/token/refresh", s.handleTokenRefresh)
	mux.HandleFunc("/api/provider", s.handleProvider)
	mux.HandleFunc("/api/model-roles", s.handleModelRoles)
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/auth/route", s.handleAuthRoute)
	mux.HandleFunc("/api/model-tiers", s.handleModelTiers)
	mux.HandleFunc("/api/stats/daily", s.handleStatsByDay)
	mux.HandleFunc("/api/stats/hourly", s.handleStatsByHour)
	mux.HandleFunc("/api/stats/routes", s.handleStatsByRoute)
	mux.HandleFunc("/api/stats/tokens", s.handleTokenTotals)
	mux.HandleFunc("/api/provider/delete-model", s.handleDeleteModel)
	mux.HandleFunc("/api/provider/add-model", s.handleAddModel)
	mux.HandleFunc("/api/amp-config", s.handleAmpConfig)
	mux.HandleFunc("/api/keys", s.handleAPIKeys)
	mux.HandleFunc("/api/keys/add", s.handleAddAPIKey)
	mux.HandleFunc("/api/keys/update", s.handleUpdateAPIKey)
	mux.HandleFunc("/api/keys/remove", s.handleRemoveAPIKey)
	mux.HandleFunc("/api/keys/test", s.handleTestAPIKey)
	mux.HandleFunc("/api/keys/discover", s.handleDiscoverModels)
	mux.HandleFunc("/api/custom-provider", s.handleCustomProvider)
	mux.HandleFunc("/api/version", s.handleVersion)
	mux.HandleFunc("/api/update/check", s.handleCheckUpdate)
	mux.HandleFunc("/api/redirects", s.handleRedirects)
	mux.HandleFunc("/api/redirects/set", s.handleSetRedirect)

	// Embedded web UI
	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to load embedded web files: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	server := &http.Server{Addr: addr, Handler: corsMiddleware(mux)}
	log.Infof("admin dashboard on http://localhost%s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Errorf("admin server error: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
