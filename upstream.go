package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"
)

// UpstreamProxy forwards requests to ampcode.com for models not in the local whitelist.
type UpstreamProxy struct {
	proxy *httputil.ReverseProxy
}

func NewUpstreamProxy(upstreamURL, apiKey string) (*UpstreamProxy, error) {
	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		return nil, fmt.Errorf("invalid upstream url %q: %w", upstreamURL, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(parsed)
	originalDirector := proxy.Director

	// Immediate flush for SSE and WebSocket streams
	proxy.FlushInterval = -1

	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = parsed.Host

		// Always replace auth with upstream AMP key for all routes.
		// The client key (sk-...) is for authenticating to our local proxy,
		// not for ampcode.com. The upstream AMP key (sgamp_user_...) is
		// what ampcode.com needs for both provider and management routes.
		req.Header.Del("Authorization")
		req.Header.Del("X-Api-Key")
		if apiKey != "" {
			req.Header.Set("X-Api-Key", apiKey)
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}

	// ModifyResponse: log upstream responses for debugging
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode >= 400 {
			log.Warnf("[UPSTREAM] %s %s → %d", resp.Request.Method, resp.Request.URL.Path, resp.StatusCode)
		}
		return nil
	}

	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Errorf("upstream proxy error: %s %s: %v", req.Method, req.URL.Path, err)
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte(`{"error":"upstream_proxy_error","message":"failed to reach AMP upstream"}`))
	}

	return &UpstreamProxy{proxy: proxy}, nil
}

func (p *UpstreamProxy) Forward(w http.ResponseWriter, r *http.Request) {
	// Swallow ErrAbortHandler panics from ReverseProxy
	defer func() {
		if rec := recover(); rec != nil {
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				return
			}
			panic(rec)
		}
	}()

	log.Debugf("[UPSTREAM] forwarding %s %s", r.Method, r.URL.Path)
	p.proxy.ServeHTTP(w, r)
}

// isWebSocketUpgrade checks if a request is a WebSocket upgrade.
func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
