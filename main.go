package main

import (
	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
)

func main() {
	log.SetLevel(log.InfoLevel)
	log.SetOutput(os.Stderr)

	cfg := loadConfig()
	log.Infof("amp-proxy starting on %s", cfg.Listen)

	claudeMgr := NewTokenManager()
	codexMgr := NewCodexTokenManager()
	geminiMgr := NewGeminiTokenManager()

	logger := NewRequestLogger()
	defer logger.Close()

	authResolver := NewAuthResolver(cfg, claudeMgr, codexMgr, geminiMgr)
	router := NewRouter(cfg, logger, authResolver)

	// Start admin dashboard
	admin := NewAdminServer(cfg, claudeMgr, logger, authResolver)
	go admin.Start(":9318")

	// Periodically flush pending log entries that never got a RecordResult
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			logger.FlushPending()
		}
	}()

	// Start proxy server
	go func() {
		server := &http.Server{
			Addr:    cfg.Listen,
			Handler: router,
		}
		log.Infof("proxy listening on %s", cfg.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("proxy server error: %v", err)
		}
	}()

	// Run systray on main thread (required by macOS)
	setupTray(cfg, claudeMgr, logger, authResolver)
}
