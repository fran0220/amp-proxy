package main

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/getlantern/systray"
	log "github.com/sirupsen/logrus"
)

func setupTray(cfg *Config, tokenMgr *TokenManager, logger *RequestLogger, authResolver *AuthResolver) {
	systray.Run(onTrayReady(cfg, tokenMgr, logger, authResolver), onTrayExit)
}

func onTrayReady(cfg *Config, tokenMgr *TokenManager, logger *RequestLogger, authResolver *AuthResolver) func() {
	return func() {
		systray.SetIcon(iconGreen)
		systray.SetTooltip("AMP Proxy")

		mStatus := systray.AddMenuItem(fmt.Sprintf("AMP Proxy - Running %s", cfg.Listen), "")
		mStatus.Disable()

		mToken := systray.AddMenuItem("Token: checking...", "")
		mToken.Disable()

		systray.AddSeparator()

		mClaude := systray.AddMenuItem("Claude  ...", "")
		mClaude.Disable()
		mOpenAI := systray.AddMenuItem("OpenAI  ...", "")
		mOpenAI.Disable()
		mGemini := systray.AddMenuItem("Gemini  ...", "")
		mGemini.Disable()

		systray.AddSeparator()

		mStats := systray.AddMenuItem("Stats: ...", "")
		mStats.Disable()

		systray.AddSeparator()

		mRefresh := systray.AddMenuItem("Reload Token", "")
		mDashboard := systray.AddMenuItem("Open Dashboard", "")

		systray.AddSeparator()

		mQuit := systray.AddMenuItem("Quit", "")

		// Initial refresh
		refreshTray(cfg, authResolver, logger, mToken, mClaude, mOpenAI, mGemini, mStats)

		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					refreshTray(cfg, authResolver, logger, mToken, mClaude, mOpenAI, mGemini, mStats)
				case <-mRefresh.ClickedCh:
					log.Info("reloading token from Keychain...")
					if err := tokenMgr.loadFromKeychain(); err != nil {
						log.Errorf("keychain reload failed: %v", err)
						mToken.SetTitle("Token: " + err.Error())
						systray.SetIcon(iconRed)
					} else {
						log.Info("token reloaded from Keychain")
						refreshTray(cfg, authResolver, logger, mToken, mClaude, mOpenAI, mGemini, mStats)
					}
				case <-mDashboard.ClickedCh:
					_ = exec.Command("open", "http://localhost:9318").Start()
				case <-mQuit.ClickedCh:
					systray.Quit()
				}
			}
		}()
	}
}

func onTrayExit() {
	log.Info("amp-proxy shutting down")
}

func refreshTray(cfg *Config, authResolver *AuthResolver, logger *RequestLogger,
	mToken, mClaude, mOpenAI, mGemini, mStats *systray.MenuItem) {

	authStatus := authResolver.AuthStatus()

	// Token (Claude keychain as primary indicator)
	claudeAuth := authStatus["claude"].(map[string]any)
	claudeLocalOK, _ := claudeAuth["local_available"].(bool)
	if claudeLocalOK {
		exp, _ := claudeAuth["local_expires_in"].(string)
		mToken.SetTitle(fmt.Sprintf("Claude Keychain: Valid (%s)", exp))
		systray.SetIcon(iconGreen)
	} else {
		msg := "unavailable"
		if e, ok := claudeAuth["local_error"].(string); ok {
			msg = e
		}
		mToken.SetTitle("Claude Keychain: " + msg)
		systray.SetIcon(iconRed)
	}

	// Providers
	cfg.mu.RLock()
	cl, ca, cu, ct := countRoutes(cfg.Claude.Models)
	ol, oa, ou, ot := countRoutes(cfg.OpenAI.Models)
	gl, ga, gu, gt := countRoutes(cfg.Gemini.Models)
	cfg.mu.RUnlock()

	mClaude.SetTitle(fmtProviderLine("Claude", cl, ca, cu, ct, authStatus["claude"]))
	mOpenAI.SetTitle(fmtProviderLine("OpenAI", ol, oa, ou, ot, authStatus["openai"]))
	mGemini.SetTitle(fmtProviderLine("Gemini", gl, ga, gu, gt, authStatus["gemini"]))

	// Stats
	stats := logger.GetStats()
	totalTokens := stats.TotalInputTokens + stats.TotalOutputTokens
	mStats.SetTitle(fmt.Sprintf("Stats: %d reqs | %s tokens", stats.TotalRequests, fmtTokensTray(totalTokens)))
}

func fmtProviderLine(name string, local, apikey, amp, total int, authInfo any) string {
	info, _ := authInfo.(map[string]any)
	localOK, _ := info["local_available"].(bool)
	apikeyOK, _ := info["apikey_available"].(bool)

	sources := []string{}
	if localOK && local > 0 {
		sources = append(sources, fmt.Sprintf("%dL", local))
	}
	if apikeyOK && apikey > 0 {
		sources = append(sources, fmt.Sprintf("%dK", apikey))
	}
	if amp > 0 {
		sources = append(sources, fmt.Sprintf("%dA", amp))
	}

	dot := "○"
	if localOK || apikeyOK {
		dot = "●"
	}

	summary := ""
	if len(sources) > 0 {
		for i, s := range sources {
			if i > 0 {
				summary += " "
			}
			summary += s
		}
	}

	return fmt.Sprintf("%s %s  %d models  [%s]", dot, name, total, summary)
}

func fmtTokensTray(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}
