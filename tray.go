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

		mStatus := systray.AddMenuItem(fmt.Sprintf("AMP Proxy %s - Running %s", version, cfg.Listen), "")
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
		mUpdate := systray.AddMenuItem("Check for Updates", "")

		systray.AddSeparator()

		mQuit := systray.AddMenuItem("Quit", "")

		// Initial refresh
		refreshTray(cfg, authResolver, logger, mToken, mClaude, mOpenAI, mGemini, mStats)

		// Background auto-check for updates
		go func() {
			time.Sleep(10 * time.Second)
			updater := NewUpdater()
			info, err := updater.Check()
			if err == nil && info.Available {
				mUpdate.SetTitle("Update to " + info.LatestVersion + " available")
			}
		}()

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
				case <-mUpdate.ClickedCh:
					go func() {
						mUpdate.SetTitle("Checking...")
						mUpdate.Disable()
						updater := NewUpdater()
						info, err := updater.Check()
						if err != nil {
							log.Errorf("update check failed: %v", err)
							mUpdate.SetTitle("Update check failed")
							time.AfterFunc(5*time.Second, func() {
								mUpdate.SetTitle("Check for Updates")
								mUpdate.Enable()
							})
							return
						}
						if !info.Available {
							mUpdate.SetTitle("Up to date (" + version + ")")
							time.AfterFunc(5*time.Second, func() {
								mUpdate.SetTitle("Check for Updates")
								mUpdate.Enable()
							})
							return
						}
						mUpdate.SetTitle("Update to " + info.LatestVersion + "...")
						mUpdate.Enable()
						// Wait for click to install
						<-mUpdate.ClickedCh
						mUpdate.SetTitle("Downloading...")
						mUpdate.Disable()
						if err := updater.Apply(info); err != nil {
							log.Errorf("update failed: %v", err)
							mUpdate.SetTitle("Update failed: " + err.Error())
							time.AfterFunc(5*time.Second, func() {
								mUpdate.SetTitle("Check for Updates")
								mUpdate.Enable()
							})
							return
						}
						mUpdate.SetTitle("Restarting...")
						systray.Quit()
					}()
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

	// Determine overall health: green if any provider has local or apikey auth
	anyAvailable := false
	for _, key := range []string{"claude", "openai", "gemini"} {
		info, _ := authStatus[key].(map[string]any)
		if localOK, _ := info["local_available"].(bool); localOK {
			anyAvailable = true
		}
		if apikeyOK, _ := info["apikey_available"].(bool); apikeyOK {
			anyAvailable = true
		}
	}
	if anyAvailable {
		systray.SetIcon(iconGreen)
	} else {
		systray.SetIcon(iconRed)
	}

	// Token line: show Claude keychain status as informational
	claudeAuth := authStatus["claude"].(map[string]any)
	claudeLocalOK, _ := claudeAuth["local_available"].(bool)
	if claudeLocalOK {
		exp, _ := claudeAuth["local_expires_in"].(string)
		mToken.SetTitle(fmt.Sprintf("Claude Keychain: Valid (%s)", exp))
	} else {
		msg := "unavailable"
		if e, ok := claudeAuth["local_error"].(string); ok {
			msg = e
		}
		mToken.SetTitle("Claude Keychain: " + msg)
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
