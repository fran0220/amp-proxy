package main

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type Router struct {
	cfg                  *Config
	claudeHandler        *ClaudeHandler
	openaiHandler        *OpenAIHandler
	openaiResponsesHandler *OpenAIResponsesHandler
	codexHandler         *CodexHandler
	geminiHandler        *GeminiHandler
	geminiCLIHandler     *GeminiCLIHandler
	wsResponsesHandler   *WebSocketResponsesHandler
	upstream             *UpstreamProxy
	logger               *RequestLogger
	authResolver         *AuthResolver
}

func NewRouter(cfg *Config, logger *RequestLogger, authResolver *AuthResolver) *Router {
	retryer := NewRetryer(cfg.Retry.MaxAttempts, cfg.Retry.InitialDelay)

	r := &Router{
		cfg:                    cfg,
		claudeHandler:          NewClaudeHandler(cfg, retryer, logger),
		openaiHandler:          NewOpenAIHandler(cfg, retryer, logger),
		openaiResponsesHandler: NewOpenAIResponsesHandler(cfg, retryer, logger),
		codexHandler:           NewCodexHandler(cfg, retryer, logger),
		geminiHandler:          NewGeminiHandler(cfg, retryer, logger),
		geminiCLIHandler:       NewGeminiCLIHandler(cfg, retryer, logger),
		wsResponsesHandler:     NewWebSocketResponsesHandler(cfg, authResolver, logger),
		logger:                 logger,
		authResolver:           authResolver,
	}

	if cfg.Amp.UpstreamURL != "" {
		upstream, err := NewUpstreamProxy(cfg.Amp.UpstreamURL, cfg.Amp.APIKey)
		if err != nil {
			log.Fatalf("failed to create upstream proxy: %v", err)
		}
		r.upstream = upstream
	}

	return r
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	path := r.URL.Path

	upgrade := r.Header.Get("Upgrade")
	conn := r.Header.Get("Connection")
	log.Infof("[REQ] %s %s (Upgrade=%q Connection=%q)", r.Method, path, upgrade, conn)

	// WebSocket upgrade handling — check if we can handle provider WS locally
	if isWebSocketUpgrade(r) {
		if rt.handleProviderWebSocket(w, r) {
			return
		}
		log.Infof("[UPSTREAM] WebSocket upgrade: %s %s", r.Method, path)
		rt.forwardUpstream(w, r)
		return
	}

	// Standard Anthropic API endpoint: /v1/messages
	if strings.HasPrefix(path, "/v1/messages") && r.Method == http.MethodPost {
		rt.handleStandardClaude(w, r, start)
		return
	}

	if !strings.HasPrefix(path, "/api/provider/") {
		log.Infof("[UPSTREAM] non-provider: %s %s", r.Method, path)
		rt.forwardUpstream(w, r)
		return
	}

	provider := extractProvider(path)

	if r.Method != http.MethodPost {
		rt.forwardUpstream(w, r)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		log.Errorf("failed to read request body: %v", err)
		rt.forwardUpstream(w, r)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Extract model: JSON body for OpenAI/Claude, URL path for Gemini
	model := gjson.GetBytes(bodyBytes, "model").String()
	if model == "" && provider == "google" {
		model = extractGeminiModel(path)
	}
	if model == "" {
		log.Infof("[UPSTREAM] no model in body: %s %s", r.Method, path)
		rt.forwardUpstream(w, r)
		return
	}

	// Resolve auth for this model
	auth, resolvedRoute := rt.authResolver.Resolve(r.Context(), provider, model)
	log.Infof("[ROUTE] model=%s provider=%s route=%s resolved=%s", model, provider, rt.cfg.ModelRoute(provider, model), resolvedRoute)

	if resolvedRoute != RouteAmp && auth != nil && auth.Valid() {
		routeLabel := resolvedRoute + "/" + auth.Source
		switch provider {
		case "anthropic":
			log.Infof("[%s] %s -> %s (Claude)", routeLabel, model, path)
			rt.logger.LogRequest(model, provider, routeLabel, path, start)
			rt.claudeHandler.Handle(w, r, bodyBytes, auth)
			return
		case "openai":
			rt.handleOpenAI(w, r, bodyBytes, auth, model, path, resolvedRoute, routeLabel, start)
			return
		case "google":
			rt.logger.LogRequest(model, provider, routeLabel, path, start)
			if resolvedRoute == RouteLocal && auth.Source == "gemini-file" {
				log.Infof("[%s] %s -> %s (Gemini CLI)", routeLabel, model, path)
				rt.geminiCLIHandler.Handle(w, r, bodyBytes, auth)
			} else {
				log.Infof("[%s] %s -> %s (Gemini)", routeLabel, model, path)
				rt.geminiHandler.Handle(w, r, bodyBytes, auth)
			}
			return
		}
	}

	log.Infof("[UPSTREAM] %s -> %s (route=amp or no auth)", model, path)
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	rt.forwardUpstream(w, r)
	rt.logger.LogRequest(model, "upstream", "UPSTREAM", path, start)
}

// handleOpenAI routes OpenAI requests to the correct handler based on path and auth source.
// - /responses path + codex-file auth → CodexHandler (chatgpt.com backend)
// - /responses path + api-key auth → OpenAIResponsesHandler (api.openai.com/v1/responses)
// - /chat/completions or other paths → OpenAIHandler (standard OpenAI)
func (rt *Router) handleOpenAI(w http.ResponseWriter, r *http.Request, body []byte, auth *ProviderAuth, model, path, resolvedRoute, routeLabel string, start time.Time) {
	// LogRequest must be called before Handle so RecordResult can find the pending entry.
	rt.logger.LogRequest(model, "openai", routeLabel, path, start)

	isResponsesPath := isResponsesAPIPath(path)

	if isResponsesPath {
		if resolvedRoute == RouteLocal && auth.Source == "codex-file" {
			log.Infof("[%s] %s -> %s (Codex CLI Responses)", routeLabel, model, path)
			rt.codexHandler.Handle(w, r, body, auth)
		} else {
			log.Infof("[%s] %s -> %s (OpenAI Responses API)", routeLabel, model, path)
			rt.openaiResponsesHandler.Handle(w, r, body, auth)
		}
	} else {
		if resolvedRoute == RouteLocal && auth.Source == "codex-file" {
			log.Infof("[%s] %s -> %s (Codex CLI)", routeLabel, model, path)
			rt.codexHandler.Handle(w, r, body, auth)
		} else {
			log.Infof("[%s] %s -> %s (OpenAI)", routeLabel, model, path)
			rt.openaiHandler.Handle(w, r, body, auth)
		}
	}
}

// handleProviderWebSocket checks if a WebSocket upgrade on a provider path can be
// handled locally (e.g., OpenAI Responses API with local credentials or API key).
// Returns true if handled, false if the caller should fall back to upstream.
func (rt *Router) handleProviderWebSocket(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/provider/") {
		return false
	}

	provider := extractProvider(path)

	// Only handle OpenAI Responses API WebSocket locally
	if provider == "openai" && isResponsesAPIPath(path) {
		if rt.wsResponsesHandler.CanHandle(r) {
			log.Infof("[WS-RESPONSES] handling locally: %s %s", r.Method, path)
			rt.wsResponsesHandler.Handle(w, r)
			return true
		}
		log.Infof("[WS-RESPONSES] no local auth, falling back to upstream: %s %s", r.Method, path)
	}

	return false
}

func (rt *Router) forwardUpstream(w http.ResponseWriter, r *http.Request) {
	if rt.upstream == nil {
		http.Error(w, `{"error":"no upstream configured"}`, http.StatusBadGateway)
		return
	}
	rt.upstream.Forward(w, r)
}

// handleStandardClaude handles standard Anthropic API requests at /v1/messages.
// This allows any program that speaks the Anthropic API to use local credentials.
func (rt *Router) handleStandardClaude(w http.ResponseWriter, r *http.Request, start time.Time) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":{"type":"proxy_error","message":"failed to read body"}}`, http.StatusBadRequest)
		return
	}

	model := gjson.GetBytes(bodyBytes, "model").String()
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	auth, resolvedRoute := rt.authResolver.Resolve(r.Context(), "anthropic", model)
	if auth == nil || !auth.Valid() {
		log.Warnf("[STANDARD-API] no auth available for %s (route=%s)", model, resolvedRoute)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"type":"auth_error","message":"no local credentials available for Claude"}}`))
		return
	}

	routeLabel := resolvedRoute + "/" + auth.Source
	log.Infof("[STANDARD-API] %s -> /v1/messages (%s)", model, routeLabel)
	rt.logger.LogRequest(model, "anthropic", routeLabel, r.URL.Path, start)
	rt.claudeHandler.Handle(w, r, bodyBytes, auth)
}

func extractProvider(path string) string {
	parts := strings.SplitN(path, "/", 5)
	if len(parts) >= 4 {
		return strings.ToLower(parts[3])
	}
	return ""
}

// isResponsesAPIPath checks if the request path targets the OpenAI Responses API.
// Matches paths like:
//   - /api/provider/openai/v1/responses
//   - /api/provider/openai/responses
func isResponsesAPIPath(path string) bool {
	// Strip provider prefix: /api/provider/openai/...
	const prefix = "/api/provider/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// Find the sub-path after provider name
	rest := path[len(prefix):]
	if idx := strings.Index(rest, "/"); idx >= 0 {
		subPath := rest[idx:]
		return subPath == "/v1/responses" || subPath == "/responses" ||
			strings.HasPrefix(subPath, "/v1/responses?") || strings.HasPrefix(subPath, "/responses?")
	}
	return false
}
