package main

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	RouteAmp    = "amp"
	RouteLocal  = "local"
	RouteAPIKey = "apikey"

	AuthBearer     = "bearer"
	AuthXAPIKey    = "x-api-key"
	AuthGoogAPIKey = "x-goog-api-key"
)

// ProviderAuth holds resolved credentials for a single request.
type ProviderAuth struct {
	Token    string
	AuthType string // "bearer", "x-api-key", "x-goog-api-key"
	Source   string // "keychain", "codex-file", "gemini-file", "api-key"
	Email    string
	Expires  time.Time
	BaseURL  string // per-key base URL override
	Error    error
}

func (a *ProviderAuth) Valid() bool {
	return a != nil && a.Token != "" && a.Error == nil
}

// AuthResolver resolves credentials for a given provider and route.
type AuthResolver struct {
	cfg       *Config
	claudeMgr *TokenManager
	codexMgr  *CodexTokenManager
	geminiMgr *GeminiTokenManager
}

func NewAuthResolver(cfg *Config, claudeMgr *TokenManager, codexMgr *CodexTokenManager, geminiMgr *GeminiTokenManager) *AuthResolver {
	return &AuthResolver{cfg: cfg, claudeMgr: claudeMgr, codexMgr: codexMgr, geminiMgr: geminiMgr}
}

// Resolve returns credentials for a provider+model, following the route config.
// If the requested route's source is unavailable, falls back: local → apikey → amp.
// Also checks tier compatibility (e.g., image models not available via Gemini CLI).
func (ar *AuthResolver) Resolve(ctx context.Context, provider, model string) (*ProviderAuth, string) {
	route := ar.cfg.ModelRoute(provider, model)
	if route == RouteAmp {
		return nil, RouteAmp
	}

	// Check tier compatibility
	if !ModelSupportsTier(model, route) {
		log.Warnf("[AUTH] %s/%s not supported on tier %s, falling back to amp", provider, model, route)
		return nil, RouteAmp
	}

	auth := ar.resolveRoute(ctx, provider, route)
	if auth.Valid() {
		return auth, route
	}

	// Fallback chain: local → apikey → amp
	if route == RouteLocal {
		log.Warnf("[AUTH] %s/%s local unavailable (%v), trying apikey", provider, model, auth.Error)
		if ModelSupportsTier(model, RouteAPIKey) {
			fallback := ar.resolveRoute(ctx, provider, RouteAPIKey)
			if fallback.Valid() {
				return fallback, RouteAPIKey
			}
		}
	}

	log.Warnf("[AUTH] %s/%s no credentials available, falling back to amp", provider, model)
	return nil, RouteAmp
}

func (ar *AuthResolver) resolveRoute(ctx context.Context, provider, route string) *ProviderAuth {
	switch provider {
	case "anthropic", "claude":
		return ar.resolveClaude(ctx, route)
	case "openai", "codex":
		return ar.resolveOpenAI(ctx, route)
	case "google", "gemini":
		return ar.resolveGemini(ctx, route)
	default:
		return &ProviderAuth{Error: fmt.Errorf("unknown provider: %s", provider)}
	}
}

func (ar *AuthResolver) resolveClaude(ctx context.Context, route string) *ProviderAuth {
	switch route {
	case RouteLocal:
		token, err := ar.claudeMgr.GetAccessToken(ctx)
		if err != nil {
			return &ProviderAuth{Error: err, Source: "keychain"}
		}
		status := ar.claudeMgr.Status()
		return &ProviderAuth{
			Token:    token,
			AuthType: AuthBearer,
			Source:   "keychain",
			Expires:  time.Now().Add(status.ExpiresIn),
		}
	case RouteAPIKey:
		entry, ok := ar.cfg.PreferredAPIKey("claude")
		if !ok {
			return &ProviderAuth{Error: fmt.Errorf("claude api-key not configured"), Source: "api-key"}
		}
		return &ProviderAuth{Token: entry.APIKey, AuthType: AuthXAPIKey, Source: "api-key", BaseURL: entry.BaseURL}
	default:
		return &ProviderAuth{Error: fmt.Errorf("invalid route: %s", route)}
	}
}

func (ar *AuthResolver) resolveOpenAI(ctx context.Context, route string) *ProviderAuth {
	switch route {
	case RouteLocal:
		if ar.codexMgr == nil {
			return &ProviderAuth{Error: fmt.Errorf("codex token manager not initialized"), Source: "codex-file"}
		}
		token, err := ar.codexMgr.GetAccessToken(ctx)
		if err != nil {
			return &ProviderAuth{Error: err, Source: "codex-file"}
		}
		status := ar.codexMgr.Status()
		return &ProviderAuth{
			Token:    token,
			AuthType: AuthBearer,
			Source:   "codex-file",
			Email:    status.Email,
			Expires:  time.Now().Add(status.ExpiresIn),
		}
	case RouteAPIKey:
		entry, ok := ar.cfg.PreferredAPIKey("openai")
		if !ok {
			return &ProviderAuth{Error: fmt.Errorf("openai api-key not configured"), Source: "api-key"}
		}
		return &ProviderAuth{Token: entry.APIKey, AuthType: AuthBearer, Source: "api-key", BaseURL: entry.BaseURL}
	default:
		return &ProviderAuth{Error: fmt.Errorf("invalid route: %s", route)}
	}
}

func (ar *AuthResolver) resolveGemini(ctx context.Context, route string) *ProviderAuth {
	switch route {
	case RouteLocal:
		if ar.geminiMgr == nil {
			return &ProviderAuth{Error: fmt.Errorf("gemini token manager not initialized"), Source: "gemini-file"}
		}
		token, err := ar.geminiMgr.GetAccessToken(ctx)
		if err != nil {
			return &ProviderAuth{Error: err, Source: "gemini-file"}
		}
		status := ar.geminiMgr.Status()
		return &ProviderAuth{
			Token:    token,
			AuthType: AuthBearer,
			Source:   "gemini-file",
			Email:    status.Email,
			Expires:  time.Now().Add(status.ExpiresIn),
		}
	case RouteAPIKey:
		entry, ok := ar.cfg.PreferredAPIKey("gemini")
		if !ok {
			return &ProviderAuth{Error: fmt.Errorf("gemini api-key not configured"), Source: "api-key"}
		}
		return &ProviderAuth{Token: entry.APIKey, AuthType: AuthGoogAPIKey, Source: "api-key", BaseURL: entry.BaseURL}
	default:
		return &ProviderAuth{Error: fmt.Errorf("invalid route: %s", route)}
	}
}

// AuthStatus returns a summary of available auth sources per provider.
func (ar *AuthResolver) AuthStatus() map[string]any {
	claudeStatus := ar.claudeMgr.Status()
	claude := map[string]any{
		"local_available": claudeStatus.Valid,
		"local_source":    "keychain",
	}
	if claudeStatus.Valid {
		claude["local_expires_in"] = claudeStatus.ExpiresIn.Round(time.Second).String()
	}
	if claudeStatus.Error != nil {
		claude["local_error"] = claudeStatus.Error.Error()
	}
	claude["apikey_available"] = len(ar.cfg.AllAPIKeys("claude")) > 0

	openai := map[string]any{"local_source": "codex-file"}
	if ar.codexMgr != nil {
		cs := ar.codexMgr.Status()
		openai["local_available"] = cs.Valid
		if cs.Valid {
			openai["local_expires_in"] = cs.ExpiresIn.Round(time.Second).String()
		}
		if cs.Email != "" {
			openai["local_email"] = cs.Email
		}
		if cs.Error != nil {
			openai["local_error"] = cs.Error.Error()
		}
	} else {
		openai["local_available"] = false
	}
	openai["apikey_available"] = len(ar.cfg.AllAPIKeys("openai")) > 0

	gemini := map[string]any{"local_source": "gemini-file"}
	if ar.geminiMgr != nil {
		gs := ar.geminiMgr.Status()
		gemini["local_available"] = gs.Valid
		if gs.Valid {
			gemini["local_expires_in"] = gs.ExpiresIn.Round(time.Second).String()
		}
		if gs.Email != "" {
			gemini["local_email"] = gs.Email
		}
		if gs.Error != nil {
			gemini["local_error"] = gs.Error.Error()
		}
	} else {
		gemini["local_available"] = false
	}
	gemini["apikey_available"] = len(ar.cfg.AllAPIKeys("gemini")) > 0

	return map[string]any{
		"claude": claude,
		"openai": openai,
		"gemini": gemini,
	}
}
