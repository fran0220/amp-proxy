package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

func (s *AdminServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats := s.logger.GetStats()

	status := map[string]any{
		"running":        true,
		"version":        version,
		"uptime":         time.Since(s.startAt).Round(time.Second).String(),
		"listen":         s.cfg.Listen,
		"upstream":       s.cfg.Amp.UpstreamURL,
		"total_requests": stats.TotalRequests,
		"total_errors":   stats.TotalErrors,
		"total_input":    stats.TotalInputTokens,
		"total_output":   stats.TotalOutputTokens,
		"auth":           s.authResolver.AuthStatus(),
	}
	writeJSON(w, status)
}

func (s *AdminServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": version})
}

func (s *AdminServer) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	updater := NewUpdater()
	info, err := updater.Check()
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, info)
}

func (s *AdminServer) handleOverview(w http.ResponseWriter, r *http.Request) {
	stats := s.logger.GetStats()
	recentLogs := s.logger.GetLogs(10, 0)

	s.cfg.mu.RLock()
	claudeLocal, claudeAPIKey, claudeAmp, claudeTotal := countRoutes(s.cfg.Claude.Models)
	openaiLocal, openaiAPIKey, openaiAmp, openaiTotal := countRoutes(s.cfg.OpenAI.Models)
	geminiLocal, geminiAPIKey, geminiAmp, geminiTotal := countRoutes(s.cfg.Gemini.Models)
	s.cfg.mu.RUnlock()

	authStatus := s.authResolver.AuthStatus()

	overview := map[string]any{
		"uptime": time.Since(s.startAt).Round(time.Second).String(),
		"stats":  stats,
		"recent": recentLogs,
		"providers": map[string]any{
			"claude": map[string]any{
				"local": claudeLocal, "apikey": claudeAPIKey, "amp": claudeAmp, "total": claudeTotal,
				"auth": authStatus["claude"],
			},
			"openai": map[string]any{
				"local": openaiLocal, "apikey": openaiAPIKey, "amp": openaiAmp, "total": openaiTotal,
				"auth": authStatus["openai"],
			},
			"gemini": map[string]any{
				"local": geminiLocal, "apikey": geminiAPIKey, "amp": geminiAmp, "total": geminiTotal,
				"auth": authStatus["gemini"],
			},
		},
		"upstream": s.cfg.Amp.UpstreamURL,
	}
	writeJSON(w, overview)
}

func (s *AdminServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.cfg.mu.RLock()
	defer s.cfg.mu.RUnlock()

	safe := map[string]any{
		"listen": s.cfg.Listen,
		"amp":    map[string]any{"upstream-url": s.cfg.Amp.UpstreamURL, "has-key": s.cfg.Amp.APIKey != ""},
		"claude": map[string]any{
			"source": s.cfg.Claude.Source, "has-key": s.cfg.Claude.APIKey != "",
			"models":  s.cfg.Claude.Models,
			"entries": maskKeys(s.cfg.allAPIKeysUnlocked("claude")),
		},
		"openai": map[string]any{
			"has-key": s.cfg.OpenAI.APIKey != "", "has-url": s.cfg.OpenAI.BaseURL != "",
			"models":  s.cfg.OpenAI.Models,
			"entries": maskKeys(s.cfg.allAPIKeysUnlocked("openai")),
		},
		"gemini": map[string]any{
			"has-key": s.cfg.Gemini.APIKey != "", "has-url": s.cfg.Gemini.BaseURL != "",
			"models":  s.cfg.Gemini.Models,
			"entries": maskKeys(s.cfg.allAPIKeysUnlocked("gemini")),
		},
		"custom": s.getCustomProvidersSafeUnlocked(),
	}
	writeJSON(w, safe)
}

func (s *AdminServer) handleConfigModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Route    string `json:"route"`
		Enabled  *bool  `json:"enabled,omitempty"` // backward compat
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Backward compat: if "enabled" is sent without "route", convert
	if req.Route == "" && req.Enabled != nil {
		if *req.Enabled {
			req.Route = "local"
		} else {
			req.Route = "amp"
		}
	}

	if req.Route != "" {
		s.cfg.SetModelRoute(req.Provider, req.Model, req.Route)
	}

	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("[ADMIN] set route %s/%s -> %s", req.Provider, req.Model, req.Route)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger.GetStatsFiltered(parseStatsFilter(r)))
}

func (s *AdminServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	provider := r.URL.Query().Get("provider")
	route := r.URL.Query().Get("route")
	minStatus, _ := strconv.Atoi(r.URL.Query().Get("status"))

	if provider != "" || route != "" || minStatus > 0 {
		writeJSON(w, s.logger.GetLogsFiltered(limit, offset, provider, route, minStatus))
	} else {
		writeJSON(w, s.logger.GetLogs(limit, offset))
	}
}

func (s *AdminServer) handleErrors(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	writeJSON(w, s.logger.GetErrors(limit))
}

func (s *AdminServer) handleTokenRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.tokenMgr.refresh(r.Context()); err != nil {
		writeJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleProvider(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider string       `json:"provider"`
		APIKey   string       `json:"api_key,omitempty"`
		BaseURL  string       `json:"base_url,omitempty"`
		Models   []ModelEntry `json:"models,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.cfg.mu.Lock()
	switch req.Provider {
	case "claude", "anthropic":
		if req.APIKey != "" {
			s.cfg.Claude.APIKey = req.APIKey
		}
	case "openai":
		if req.APIKey != "" {
			s.cfg.OpenAI.APIKey = req.APIKey
		}
		if req.BaseURL != "" {
			s.cfg.OpenAI.BaseURL = req.BaseURL
		}
		if len(req.Models) > 0 {
			s.cfg.OpenAI.Models = req.Models
		}
	case "gemini", "google":
		if req.APIKey != "" {
			s.cfg.Gemini.APIKey = req.APIKey
		}
		if req.BaseURL != "" {
			s.cfg.Gemini.BaseURL = req.BaseURL
		}
		if len(req.Models) > 0 {
			s.cfg.Gemini.Models = req.Models
		}
	}
	s.cfg.mu.Unlock()

	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] updated provider: %s", req.Provider)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleModelRoles(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, AmpModelRoles)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func countRoutes(models []ModelEntry) (local, apikey, amp, total int) {
	total = len(models)
	for _, m := range models {
		switch m.Route {
		case RouteLocal:
			local++
		case RouteAPIKey:
			apikey++
		default:
			amp++
		}
	}
	return
}

func (s *AdminServer) handleAmpConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.cfg.mu.RLock()
		masked := s.cfg.Amp.APIKey
		if len(masked) > 12 {
			masked = masked[:8] + "..." + masked[len(masked)-4:]
		}
		result := map[string]any{
			"upstream_url": s.cfg.Amp.UpstreamURL,
			"api_key":      masked,
			"has_key":      s.cfg.Amp.APIKey != "",
		}
		s.cfg.mu.RUnlock()
		writeJSON(w, result)
	case http.MethodPost:
		var req struct {
			UpstreamURL string `json:"upstream_url"`
			APIKey      string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.cfg.mu.Lock()
		if req.UpstreamURL != "" {
			s.cfg.Amp.UpstreamURL = req.UpstreamURL
		}
		if req.APIKey != "" {
			s.cfg.Amp.APIKey = req.APIKey
		}
		s.cfg.mu.Unlock()
		if err := s.cfg.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("[ADMIN] updated AMP upstream config")
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *AdminServer) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.authResolver.AuthStatus())
}

func (s *AdminServer) handleModelTiers(w http.ResponseWriter, r *http.Request) {
	tiers := make(map[string][]string, len(AmpModelRoles))
	for _, role := range AmpModelRoles {
		tiers[role.Model] = role.Tiers
	}
	writeJSON(w, tiers)
}

func (s *AdminServer) handleStatsByDay(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	writeJSON(w, s.logger.GetStatsByDayFiltered(days, parseStatsFilter(r)))
}

func (s *AdminServer) handleStatsByHour(w http.ResponseWriter, r *http.Request) {
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	writeJSON(w, s.logger.GetStatsByHourFiltered(hours, parseStatsFilter(r)))
}

func (s *AdminServer) handleStatsByRoute(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger.GetStatsByRouteFiltered(parseStatsFilter(r)))
}

func (s *AdminServer) handleTokenTotals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.logger.GetTokenTotalsFiltered(parseStatsFilter(r)))
}

func (s *AdminServer) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.cfg.mu.Lock()
	models := s.cfg.modelsRefForProvider(req.Provider)
	if models != nil {
		var filtered []ModelEntry
		for _, m := range *models {
			if m.Name != req.Model {
				filtered = append(filtered, m)
			}
		}
		*models = filtered
	}
	s.cfg.mu.Unlock()

	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] deleted model %s/%s", req.Provider, req.Model)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleAddModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Route    string `json:"route"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Route == "" {
		req.Route = "amp"
	}

	s.cfg.mu.Lock()
	models := s.cfg.modelsRefForProvider(req.Provider)
	if models != nil {
		found := false
		for _, m := range *models {
			if m.Name == req.Model {
				found = true
				break
			}
		}
		if !found {
			*models = append(*models, ModelEntry{Name: req.Model, Route: req.Route})
		}
	}
	s.cfg.mu.Unlock()

	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] added model %s/%s route=%s", req.Provider, req.Model, req.Route)
	writeJSON(w, map[string]string{"status": "ok"})
}

func maskKeys(entries []APIKeyEntry) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, e := range entries {
		masked := e.APIKey
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		result[i] = map[string]any{
			"id":       e.ID,
			"label":    e.Label,
			"api_key":  masked,
			"base_url": e.BaseURL,
			"has_key":  e.APIKey != "",
		}
	}
	return result
}

func (s *AdminServer) getCustomProvidersSafeUnlocked() []map[string]any {
	result := make([]map[string]any, len(s.cfg.Custom))
	for i, cp := range s.cfg.Custom {
		result[i] = map[string]any{
			"id":       cp.ID,
			"name":     cp.Name,
			"base_url": cp.BaseURL,
			"entries":  maskKeys(cp.Entries),
			"models":   cp.Models,
		}
	}
	return result
}

func (s *AdminServer) getCustomProvidersSafe() []map[string]any {
	s.cfg.mu.RLock()
	defer s.cfg.mu.RUnlock()
	return s.getCustomProvidersSafeUnlocked()
}

func parseStatsFilter(r *http.Request) StatsFilter {
	q := r.URL.Query()
	filter := StatsFilter{
		Provider: strings.TrimSpace(q.Get("provider")),
		Route:    strings.TrimSpace(q.Get("route")),
		Model:    strings.TrimSpace(q.Get("model")),
	}

	if window := strings.TrimSpace(q.Get("window")); window != "" && window != "all" {
		if d, ok := parseTimeWindow(window); ok {
			filter.Since = time.Now().Add(-d)
		}
	}
	if from := strings.TrimSpace(q.Get("from")); from != "" {
		if ts, err := time.Parse(time.RFC3339, from); err == nil {
			filter.Since = ts
		}
	}
	if to := strings.TrimSpace(q.Get("to")); to != "" {
		if ts, err := time.Parse(time.RFC3339, to); err == nil {
			filter.Until = ts
		}
	}
	return filter
}

func parseTimeWindow(window string) (time.Duration, bool) {
	switch strings.ToLower(window) {
	case "24h":
		return 24 * time.Hour, true
	case "7d":
		return 7 * 24 * time.Hour, true
	case "14d":
		return 14 * 24 * time.Hour, true
	case "30d":
		return 30 * 24 * time.Hour, true
	case "90d":
		return 90 * 24 * time.Hour, true
	default:
		return 0, false
	}
}

func (s *AdminServer) handleAPIKeys(w http.ResponseWriter, r *http.Request) {
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		result := map[string]any{
			"claude": maskKeys(s.cfg.AllAPIKeys("claude")),
			"openai": maskKeys(s.cfg.AllAPIKeys("openai")),
			"gemini": maskKeys(s.cfg.AllAPIKeys("gemini")),
			"custom": s.getCustomProvidersSafe(),
		}
		writeJSON(w, result)
		return
	}
	writeJSON(w, maskKeys(s.cfg.AllAPIKeys(provider)))
}

func (s *AdminServer) handleAddAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Label    string `json:"label"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.APIKey == "" {
		http.Error(w, "api_key is required", http.StatusBadRequest)
		return
	}
	entry := APIKeyEntry{Label: req.Label, APIKey: req.APIKey, BaseURL: req.BaseURL}
	s.cfg.AddAPIKey(req.Provider, entry)
	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] added API key for %s (label=%s)", req.Provider, req.Label)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleUpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string  `json:"provider"`
		ID       string  `json:"id"`
		Label    string  `json:"label"`
		APIKey   *string `json:"api_key,omitempty"`
		BaseURL  string  `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Provider == "" || req.ID == "" {
		http.Error(w, "provider and id are required", http.StatusBadRequest)
		return
	}
	if !s.cfg.UpdateAPIKey(req.Provider, req.ID, req.Label, req.BaseURL, req.APIKey) {
		http.Error(w, "api key not found", http.StatusNotFound)
		return
	}
	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] updated API key %s/%s", req.Provider, req.ID)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) resolveStoredAPIKey(provider, id, customID string) (APIKeyEntry, string, bool) {
	if provider == "custom" {
		cp, ok := s.cfg.CustomProvider(customID)
		if !ok || len(cp.Entries) == 0 {
			return APIKeyEntry{}, "", false
		}
		entry := cp.Entries[0]
		baseURL := entry.BaseURL
		if baseURL == "" {
			baseURL = cp.BaseURL
		}
		return entry, baseURL, true
	}

	entry, ok := s.cfg.APIKey(provider, id)
	if !ok {
		return APIKeyEntry{}, "", false
	}
	return entry, entry.BaseURL, true
}

func (s *AdminServer) handleRemoveAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.RemoveAPIKey(req.Provider, req.ID)
	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Infof("[ADMIN] removed API key %s/%s", req.Provider, req.ID)
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *AdminServer) handleTestAPIKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		ID       string `json:"id"`
		CustomID string `json:"custom_id"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// If ID is provided, look up the stored key
	if req.Provider == "custom" && req.CustomID != "" && req.APIKey == "" {
		if entry, baseURL, ok := s.resolveStoredAPIKey(req.Provider, req.ID, req.CustomID); ok {
			req.APIKey = entry.APIKey
			if req.BaseURL == "" {
				req.BaseURL = baseURL
			}
		}
	} else if req.ID != "" && req.APIKey == "" {
		if entry, baseURL, ok := s.resolveStoredAPIKey(req.Provider, req.ID, req.CustomID); ok {
			req.APIKey = entry.APIKey
			if req.BaseURL == "" {
				req.BaseURL = baseURL
			}
		}
	}
	if req.Provider == "custom" && req.CustomID != "" && req.BaseURL == "" {
		if _, baseURL, ok := s.resolveStoredAPIKey(req.Provider, req.ID, req.CustomID); ok {
			req.BaseURL = baseURL
		}
	}
	result := testAPIKey(req.Provider, req.APIKey, req.BaseURL)
	writeJSON(w, result)
}

func (s *AdminServer) handleDiscoverModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		CustomID string `json:"custom_id"`
		APIKey   string `json:"api_key"`
		BaseURL  string `json:"base_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// If no key provided, use the preferred key for this provider.
	if req.APIKey == "" {
		if req.Provider == "custom" && req.CustomID != "" {
			if entry, baseURL, ok := s.resolveStoredAPIKey(req.Provider, "", req.CustomID); ok {
				req.APIKey = entry.APIKey
				if req.BaseURL == "" {
					req.BaseURL = baseURL
				}
			}
		} else if entry, ok := s.cfg.PreferredAPIKey(req.Provider); ok {
			req.APIKey = entry.APIKey
			if req.BaseURL == "" {
				req.BaseURL = entry.BaseURL
			}
		}
	} else if req.Provider == "custom" && req.CustomID != "" && req.BaseURL == "" {
		if _, baseURL, ok := s.resolveStoredAPIKey(req.Provider, "", req.CustomID); ok {
			req.BaseURL = baseURL
		}
	}
	models := discoverModels(req.Provider, req.APIKey, req.BaseURL)
	writeJSON(w, models)
}

func (s *AdminServer) handleCustomProvider(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, s.getCustomProvidersSafe())
	case http.MethodPost:
		var req struct {
			ID      string `json:"id,omitempty"`
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ID != "" {
			var apiKey *string
			if req.APIKey != "" {
				apiKey = &req.APIKey
			}
			if !s.cfg.UpdateCustomProvider(req.ID, req.Name, req.BaseURL, apiKey) {
				http.Error(w, "custom provider not found", http.StatusNotFound)
				return
			}
			if err := s.cfg.Save(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			log.Infof("[ADMIN] updated custom provider: %s", req.ID)
			writeJSON(w, map[string]string{"status": "ok", "id": req.ID})
			return
		}
		s.cfg.mu.Lock()
		cp := CustomProvider{
			ID:      generateID(),
			Name:    req.Name,
			BaseURL: req.BaseURL,
		}
		if req.APIKey != "" {
			cp.Entries = []APIKeyEntry{{ID: generateID(), APIKey: req.APIKey}}
		}
		s.cfg.Custom = append(s.cfg.Custom, cp)
		s.cfg.mu.Unlock()
		if err := s.cfg.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("[ADMIN] added custom provider: %s (%s)", req.Name, req.BaseURL)
		writeJSON(w, map[string]string{"status": "ok", "id": cp.ID})
	case http.MethodDelete:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.cfg.mu.Lock()
		var filtered []CustomProvider
		for _, cp := range s.cfg.Custom {
			if cp.ID != req.ID {
				filtered = append(filtered, cp)
			}
		}
		s.cfg.Custom = filtered
		s.cfg.mu.Unlock()
		if err := s.cfg.Save(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Infof("[ADMIN] removed custom provider: %s", req.ID)
		writeJSON(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *AdminServer) handleAuthRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Route    string `json:"route"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	switch req.Route {
	case RouteAmp, RouteLocal, RouteAPIKey:
	default:
		http.Error(w, "invalid route: must be amp, local, or apikey", http.StatusBadRequest)
		return
	}

	s.cfg.SetModelRoute(req.Provider, req.Model, req.Route)
	if err := s.cfg.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Infof("[ADMIN] route %s/%s -> %s", req.Provider, req.Model, req.Route)
	writeJSON(w, map[string]string{"status": "ok"})
}
