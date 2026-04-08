package main

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

func generateID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// APIKeyEntry represents a single API key configuration for a provider.
type APIKeyEntry struct {
	ID      string `yaml:"id" json:"id"`
	Label   string `yaml:"label,omitempty" json:"label,omitempty"`
	APIKey  string `yaml:"api-key" json:"api_key"`
	BaseURL string `yaml:"base-url,omitempty" json:"base_url,omitempty"`
}

// CustomProvider represents a custom OpenAI-compatible API provider.
type CustomProvider struct {
	ID      string        `yaml:"id" json:"id"`
	Name    string        `yaml:"name" json:"name"`
	BaseURL string        `yaml:"base-url" json:"base_url"`
	Entries []APIKeyEntry `yaml:"entries,omitempty" json:"entries,omitempty"`
	Models  []ModelEntry  `yaml:"models,omitempty" json:"models,omitempty"`
}

type Config struct {
	mu     sync.RWMutex     `yaml:"-"`
	path   string           `yaml:"-"`
	Listen string           `yaml:"listen"`
	UserID string           `yaml:"user-id,omitempty"` // stable user ID for prompt caching
	Amp    AmpConfig        `yaml:"amp"`
	Claude ClaudeConfig     `yaml:"claude"`
	OpenAI OpenAIConfig     `yaml:"openai"`
	Gemini GeminiConfig     `yaml:"gemini"`
	Custom []CustomProvider `yaml:"custom,omitempty"`
	Retry  RetryConfig      `yaml:"retry"`
}

type AmpConfig struct {
	UpstreamURL string `yaml:"upstream-url"`
	APIKey      string `yaml:"api-key"`
}

type ClaudeConfig struct {
	Source  string        `yaml:"source"`            // "keychain" or "manual"
	APIKey  string        `yaml:"api-key"`           // legacy single key
	Entries []APIKeyEntry `yaml:"entries,omitempty"` // multiple keys
	Models  []ModelEntry  `yaml:"models"`
}

type OpenAIConfig struct {
	APIKey  string        `yaml:"api-key"`
	BaseURL string        `yaml:"base-url"`
	Entries []APIKeyEntry `yaml:"entries,omitempty"`
	Models  []ModelEntry  `yaml:"models"`
}

type GeminiConfig struct {
	APIKey  string        `yaml:"api-key"`
	BaseURL string        `yaml:"base-url"`
	Entries []APIKeyEntry `yaml:"entries,omitempty"`
	Models  []ModelEntry  `yaml:"models"`
}

type ModelEntry struct {
	Name  string `yaml:"name" json:"name"`
	Route string `yaml:"route" json:"route"` // "amp", "local", "apikey"
}

type RetryConfig struct {
	MaxAttempts  int           `yaml:"max-attempts"`
	InitialDelay time.Duration `yaml:"initial-delay"`
}

func defaultConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".amp-proxy")
}

func defaultConfigPath() string {
	return filepath.Join(defaultConfigDir(), "config.yaml")
}

func loadConfig() *Config {
	cfg := defaultConfig()
	cfgPath := defaultConfigPath()
	cfg.path = cfgPath

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Infof("config not found at %s, creating default", cfgPath)
			if err := cfg.Save(); err != nil {
				log.Warnf("failed to save default config: %v", err)
			}
			return cfg
		}
		log.Warnf("failed to read config: %v, using defaults", err)
		return cfg
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		log.Warnf("failed to parse config: %v, using defaults", err)
		return defaultConfig()
	}
	cfg.path = cfgPath

	// Merge: ensure all default models exist in config and remove invalid entries
	changed := mergeDefaults(cfg)

	// Generate stable user ID if not yet persisted (for prompt caching)
	if cfg.UserID == "" || !isValidClaudeUserID(cfg.UserID) {
		cfg.UserID = generateClaudeUserID()
		changed = true
		log.Info("generated stable user ID for prompt caching")
	}

	if changed {
		log.Info("config migrated with updated models, saving")
		if err := cfg.Save(); err != nil {
			log.Warnf("failed to save migrated config: %v", err)
		}
	}

	return cfg
}

// mergeDefaults ensures all default models are present and removes invalid entries.
// Returns true if any changes were made.
func mergeDefaults(cfg *Config) bool {
	defaults := defaultConfig()
	changed := false
	changed = mergeModelList(&cfg.Claude.Models, defaults.Claude.Models) || changed
	changed = mergeModelList(&cfg.OpenAI.Models, defaults.OpenAI.Models) || changed
	changed = mergeModelList(&cfg.Gemini.Models, defaults.Gemini.Models) || changed
	return changed
}

func mergeModelList(models *[]ModelEntry, defaults []ModelEntry) bool {
	changed := false

	// Remove invalid entries (empty or "undefined" names)
	var clean []ModelEntry
	for _, m := range *models {
		if m.Name == "" || m.Name == "undefined" {
			changed = true
			continue
		}
		clean = append(clean, m)
	}

	// Build set of existing model names
	existing := make(map[string]bool, len(clean))
	for _, m := range clean {
		existing[m.Name] = true
	}

	// Add missing defaults
	for _, d := range defaults {
		if !existing[d.Name] {
			clean = append(clean, d)
			changed = true
		}
	}

	// Fix empty routes (default to "amp")
	for i := range clean {
		if clean[i].Route == "" {
			clean[i].Route = "amp"
			changed = true
		}
	}

	*models = clean
	return changed
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(c.path, data, 0o644)
}

func (c *Config) modelsForProvider(provider string) []ModelEntry {
	switch provider {
	case "anthropic", "claude":
		return c.Claude.Models
	case "openai", "codex":
		return c.OpenAI.Models
	case "google", "gemini":
		return c.Gemini.Models
	default:
		return nil
	}
}

func (c *Config) modelsRefForProvider(provider string) *[]ModelEntry {
	switch provider {
	case "anthropic", "claude":
		return &c.Claude.Models
	case "openai", "codex":
		return &c.OpenAI.Models
	case "google", "gemini":
		return &c.Gemini.Models
	default:
		return nil
	}
}

// ModelRoute returns the route for a model ("amp", "local", "apikey").
// Unknown models default to "amp".
func (c *Config) ModelRoute(provider, model string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, m := range c.modelsForProvider(provider) {
		if m.Name == model {
			if m.Route == "" {
				return "amp"
			}
			return m.Route
		}
	}
	return "amp"
}

// IsModelEnabled returns true if the model route is not "amp" (backward compat).
func (c *Config) IsModelEnabled(provider, model string) bool {
	return c.ModelRoute(provider, model) != "amp"
}

// SetModelRoute sets the route for a model.
func (c *Config) SetModelRoute(provider, model, route string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	models := c.modelsRefForProvider(provider)
	if models == nil {
		return
	}

	for i, m := range *models {
		if m.Name == model {
			(*models)[i].Route = route
			return
		}
	}
	*models = append(*models, ModelEntry{Name: model, Route: route})
}

// SetModelEnabled is backward compat: enabled=true sets "local", enabled=false sets "amp".
func (c *Config) SetModelEnabled(provider, model string, enabled bool) {
	route := "amp"
	if enabled {
		route = "local"
	}
	c.SetModelRoute(provider, model, route)
}

// allAPIKeysUnlocked returns all API key entries for a provider (caller must hold lock).
func (c *Config) allAPIKeysUnlocked(provider string) []APIKeyEntry {
	var entries []APIKeyEntry
	switch provider {
	case "anthropic", "claude":
		entries = append(entries, c.Claude.Entries...)
		if c.Claude.APIKey != "" {
			found := false
			for _, e := range entries {
				if e.APIKey == c.Claude.APIKey {
					found = true
					break
				}
			}
			if !found {
				entries = append([]APIKeyEntry{{ID: "_legacy", Label: "Default", APIKey: c.Claude.APIKey}}, entries...)
			}
		}
	case "openai", "codex":
		entries = append(entries, c.OpenAI.Entries...)
		if c.OpenAI.APIKey != "" {
			found := false
			for _, e := range entries {
				if e.APIKey == c.OpenAI.APIKey {
					found = true
					break
				}
			}
			if !found {
				entries = append([]APIKeyEntry{{ID: "_legacy", Label: "Default", APIKey: c.OpenAI.APIKey, BaseURL: c.OpenAI.BaseURL}}, entries...)
			}
		}
	case "google", "gemini":
		entries = append(entries, c.Gemini.Entries...)
		if c.Gemini.APIKey != "" {
			found := false
			for _, e := range entries {
				if e.APIKey == c.Gemini.APIKey {
					found = true
					break
				}
			}
			if !found {
				entries = append([]APIKeyEntry{{ID: "_legacy", Label: "Default", APIKey: c.Gemini.APIKey, BaseURL: c.Gemini.BaseURL}}, entries...)
			}
		}
	}
	return entries
}

// AllAPIKeys returns all API key entries for a provider, including the legacy single key.
func (c *Config) AllAPIKeys(provider string) []APIKeyEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allAPIKeysUnlocked(provider)
}

func (c *Config) APIKey(provider, id string) (APIKeyEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, entry := range c.allAPIKeysUnlocked(provider) {
		if entry.ID == id {
			return entry, true
		}
	}
	return APIKeyEntry{}, false
}

// PreferredAPIKey returns the API key entry that should be used by default.
// When multiple keys exist, prefer the most recently added explicit entry.
func (c *Config) PreferredAPIKey(provider string) (APIKeyEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entries := c.allAPIKeysUnlocked(provider)
	if len(entries) == 0 {
		return APIKeyEntry{}, false
	}
	return entries[len(entries)-1], true
}

func (c *Config) UpdateAPIKey(provider, id, label, baseURL string, apiKey *string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	apply := func(entry *APIKeyEntry) {
		entry.Label = label
		entry.BaseURL = baseURL
		if apiKey != nil && *apiKey != "" {
			entry.APIKey = *apiKey
		}
	}

	switch provider {
	case "anthropic", "claude":
		if id == "_legacy" {
			if apiKey != nil && *apiKey != "" {
				c.Claude.APIKey = *apiKey
			}
			return true
		}
		for i := range c.Claude.Entries {
			if c.Claude.Entries[i].ID == id {
				apply(&c.Claude.Entries[i])
				return true
			}
		}
	case "openai", "codex":
		if id == "_legacy" {
			if apiKey != nil && *apiKey != "" {
				c.OpenAI.APIKey = *apiKey
			}
			c.OpenAI.BaseURL = baseURL
			return true
		}
		for i := range c.OpenAI.Entries {
			if c.OpenAI.Entries[i].ID == id {
				apply(&c.OpenAI.Entries[i])
				return true
			}
		}
	case "google", "gemini":
		if id == "_legacy" {
			if apiKey != nil && *apiKey != "" {
				c.Gemini.APIKey = *apiKey
			}
			c.Gemini.BaseURL = baseURL
			return true
		}
		for i := range c.Gemini.Entries {
			if c.Gemini.Entries[i].ID == id {
				apply(&c.Gemini.Entries[i])
				return true
			}
		}
	}

	return false
}

func (c *Config) CustomProvider(id string) (CustomProvider, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, cp := range c.Custom {
		if cp.ID == id {
			return cp, true
		}
	}
	return CustomProvider{}, false
}

func (c *Config) UpdateCustomProvider(id, name, baseURL string, apiKey *string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Custom {
		if c.Custom[i].ID != id {
			continue
		}
		c.Custom[i].Name = name
		c.Custom[i].BaseURL = baseURL
		if apiKey != nil && *apiKey != "" {
			if len(c.Custom[i].Entries) == 0 {
				c.Custom[i].Entries = []APIKeyEntry{{ID: generateID(), APIKey: *apiKey}}
			} else {
				c.Custom[i].Entries[0].APIKey = *apiKey
			}
		}
		return true
	}

	return false
}

// AddAPIKey adds an API key entry to a provider.
func (c *Config) AddAPIKey(provider string, entry APIKeyEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.ID == "" {
		entry.ID = generateID()
	}
	switch provider {
	case "anthropic", "claude":
		c.Claude.Entries = append(c.Claude.Entries, entry)
	case "openai", "codex":
		c.OpenAI.Entries = append(c.OpenAI.Entries, entry)
	case "google", "gemini":
		c.Gemini.Entries = append(c.Gemini.Entries, entry)
	}
}

// RemoveAPIKey removes an API key entry by ID.
func (c *Config) RemoveAPIKey(provider, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	remove := func(entries []APIKeyEntry) []APIKeyEntry {
		var result []APIKeyEntry
		for _, e := range entries {
			if e.ID != id {
				result = append(result, e)
			}
		}
		return result
	}

	switch provider {
	case "anthropic", "claude":
		if id == "_legacy" {
			c.Claude.APIKey = ""
			return
		}
		c.Claude.Entries = remove(c.Claude.Entries)
	case "openai", "codex":
		if id == "_legacy" {
			c.OpenAI.APIKey = ""
			c.OpenAI.BaseURL = ""
			return
		}
		c.OpenAI.Entries = remove(c.OpenAI.Entries)
	case "google", "gemini":
		if id == "_legacy" {
			c.Gemini.APIKey = ""
			c.Gemini.BaseURL = ""
			return
		}
		c.Gemini.Entries = remove(c.Gemini.Entries)
	}
}
