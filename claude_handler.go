package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	anthropicAPIBase    = "https://api.anthropic.com"
	defaultAntropicBeta = "claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,context-management-2025-06-27,prompt-caching-scope-2026-01-05"
)

type ClaudeHandler struct {
	cfg     *Config
	retryer *Retryer
	client  *http.Client
	logger  *RequestLogger
}

func NewClaudeHandler(cfg *Config, retryer *Retryer, logger *RequestLogger) *ClaudeHandler {
	return &ClaudeHandler{
		cfg:     cfg,
		retryer: retryer,
		client:  &http.Client{},
		logger:  logger,
	}
}

func (h *ClaudeHandler) Handle(w http.ResponseWriter, r *http.Request, body []byte, auth *ProviderAuth) {
	base := anthropicAPIBase
	if auth.BaseURL != "" {
		base = strings.TrimRight(auth.BaseURL, "/")
	}
	upstreamPath := extractAnthropicPath(r.URL.Path)
	upstreamURL := base + upstreamPath + "?beta=true"
	if r.URL.RawQuery != "" {
		upstreamURL += "&" + r.URL.RawQuery
	}

	model := gjson.GetBytes(body, "model").String()
	isStream := isStreamingRequest(r, body)

	stableUserID := h.cfg.UserID
	isStandardAPI := !strings.HasPrefix(r.URL.Path, "/api/provider/")
	if isStandardAPI && auth.AuthType == AuthBearer {
		body = injectClaudeCodeIdentity(body, stableUserID)
	}
	if !isStandardAPI {
		body = renameConflictingTools(body)
		body = injectClaudeCodeIdentity(body, stableUserID)
	}

	{
		systemPreview := gjson.GetBytes(body, "system.0.text").String()
		userID := gjson.GetBytes(body, "metadata.user_id").String()
		log.Infof("[CLAUDE-DEBUG] model=%s system[0]=%q user_id=%s body_len=%d stream=%v",
			model, truncateStr(systemPreview, 60), truncateStr(userID, 30), len(body), isStream)
		_ = os.WriteFile("/tmp/amp-last-request.json", body, 0644)
	}

	resp, err := h.retryer.Do(r.Context(), h.client, func() (*http.Request, error) {
		req, reqErr := http.NewRequest(r.Method, upstreamURL, bytes.NewReader(body))
		if reqErr != nil {
			return nil, reqErr
		}

		applyDirectClaudeHeaders(req, r, auth, isStream)
		return req, nil
	})
	if err != nil {
		log.Errorf("claude request failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error":{"type":"proxy_error","message":"%s"}}`, err.Error())))
		return
	}
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if isStream && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var usage TokenUsage
		if isStandardAPI {
			usage = h.streamResponsePassthrough(w, resp.Body)
		} else {
			usage = h.streamResponseWithRename(w, resp.Body)
		}
		h.logger.RecordResult(model, resp.StatusCode, usage, 0, "", "", "")
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		if !isStandardAPI {
			respBody = renameToolsInResponse(respBody)
		}
		_, _ = w.Write(respBody)
		usage := ParseClaudeUsage(respBody)
		errMsg := ""
		if resp.StatusCode >= 400 {
			errMsg = gjson.GetBytes(respBody, "error.message").String()
		}
		h.logger.RecordResult(model, resp.StatusCode, usage, 0, errMsg, "", string(respBody))
	}
}

func applyDirectClaudeHeaders(req *http.Request, original *http.Request, auth *ProviderAuth, stream bool) {
	if auth.AuthType == AuthXAPIKey {
		req.Header.Set("x-api-key", auth.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Anthropic-Version", headerOrDefault(original, "Anthropic-Version", "2023-06-01"))
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")

	beta := original.Header.Get("Anthropic-Beta")
	if beta == "" {
		beta = defaultAntropicBeta
	}
	if !strings.Contains(beta, "oauth") {
		beta += ",oauth-2025-04-20"
	}
	req.Header.Set("Anthropic-Beta", beta)

	req.Header.Set("X-App", "cli")
	req.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli)")
	req.Header.Set("X-Stainless-Lang", "js")
	req.Header.Set("X-Stainless-Runtime", "node")
	req.Header.Set("X-Stainless-Runtime-Version", "v22.16.0")
	req.Header.Set("X-Stainless-Package-Version", "0.80.0")
	req.Header.Set("X-Stainless-Os", "MacOS")
	req.Header.Set("X-Stainless-Arch", "arm64")
	req.Header.Set("X-Stainless-Retry-Count", "0")
	req.Header.Set("X-Stainless-Timeout", "600")
	req.Header.Set("Connection", "keep-alive")

	if stream {
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Accept-Encoding", "identity")
	} else {
		req.Header.Set("Accept", "application/json")
	}
}

// streamResponsePassthrough copies SSE stream without modification, capturing usage.
func (h *ClaudeHandler) streamResponsePassthrough(w http.ResponseWriter, body io.Reader) TokenUsage {
	flusher, ok := w.(http.Flusher)
	if !ok {
		data, _ := io.ReadAll(body)
		_, _ = w.Write(data)
		return ParseClaudeUsage(data)
	}

	var lastDataLine []byte
	scanner := bufio.NewScanner(body)
	scanner.Buffer(nil, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if bytes.HasPrefix(line, []byte("data: ")) {
			lastDataLine = make([]byte, len(line))
			copy(lastDataLine, line)
		}
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}

	if lastDataLine != nil {
		return ParseClaudeUsage(lastDataLine[len("data: "):])
	}
	return TokenUsage{}
}

// streamResponseWithRename copies SSE stream, renaming tools and capturing usage.
func (h *ClaudeHandler) streamResponseWithRename(w http.ResponseWriter, body io.Reader) TokenUsage {
	flusher, ok := w.(http.Flusher)
	if !ok {
		data, _ := io.ReadAll(body)
		data = renameToolsInResponse(data)
		_, _ = w.Write(data)
		return ParseClaudeUsage(data)
	}

	var lastDataLine []byte
	scanner := bufio.NewScanner(body)
	scanner.Buffer(nil, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		line = renameToolsInSSELine(line)
		if bytes.HasPrefix(line, []byte("data: ")) {
			lastDataLine = make([]byte, len(line))
			copy(lastDataLine, line)
		}
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}
	if err := scanner.Err(); err != nil {
		log.Warnf("SSE stream scan error: %v", err)
	}

	if lastDataLine != nil {
		return ParseClaudeUsage(lastDataLine[len("data: "):])
	}
	return TokenUsage{}
}

func extractAnthropicPath(path string) string {
	const prefix = "/api/provider/anthropic"
	if strings.HasPrefix(path, prefix) {
		return path[len(prefix):]
	}
	return path
}

func isStreamingRequest(r *http.Request, body []byte) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return true
	}
	if strings.Contains(r.Header.Get("X-Stainless-Helper-Method"), "stream") {
		return true
	}
	return bytes.Contains(body, []byte(`"stream":true`)) || bytes.Contains(body, []byte(`"stream": true`))
}

func headerOrDefault(r *http.Request, key, fallback string) string {
	if v := r.Header.Get(key); v != "" {
		return v
	}
	return fallback
}
