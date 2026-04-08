package main

import "testing"

func TestPreferredAPIKeyUsesMostRecentlyAddedEntry(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.OpenAI.APIKey = "legacy-key"
	cfg.OpenAI.BaseURL = "https://legacy.example.com"
	cfg.OpenAI.Entries = []APIKeyEntry{
		{ID: "old", APIKey: "old-key", BaseURL: "https://old.example.com"},
		{ID: "new", APIKey: "new-key", BaseURL: "https://new.example.com"},
	}

	entry, ok := cfg.PreferredAPIKey("openai")
	if !ok {
		t.Fatal("expected a preferred api key")
	}
	if entry.ID != "new" {
		t.Fatalf("expected newest entry, got %q", entry.ID)
	}

	resolver := NewAuthResolver(cfg, nil, nil, nil)
	auth := resolver.resolveOpenAI(nil, RouteAPIKey)
	if !auth.Valid() {
		t.Fatalf("expected valid auth, got %#v", auth)
	}
	if auth.Token != "new-key" {
		t.Fatalf("expected newest key token, got %q", auth.Token)
	}
	if auth.BaseURL != "https://new.example.com" {
		t.Fatalf("expected newest key base url, got %q", auth.BaseURL)
	}
}

func TestPreferredAPIKeyFallsBackToLegacyKey(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig()
	cfg.OpenAI.APIKey = "legacy-key"
	cfg.OpenAI.BaseURL = "https://legacy.example.com"
	cfg.OpenAI.Entries = nil

	entry, ok := cfg.PreferredAPIKey("openai")
	if !ok {
		t.Fatal("expected legacy api key to be preferred")
	}
	if entry.ID != "_legacy" {
		t.Fatalf("expected legacy entry, got %q", entry.ID)
	}
	if entry.APIKey != "legacy-key" {
		t.Fatalf("expected legacy key, got %q", entry.APIKey)
	}
	if entry.BaseURL != "https://legacy.example.com" {
		t.Fatalf("expected legacy base url, got %q", entry.BaseURL)
	}
}
