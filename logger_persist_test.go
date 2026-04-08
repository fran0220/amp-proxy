package main

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestDBStore(t *testing.T) *DBStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &DBStore{db: db}
}

func TestQueryStatsKeepsProviderModelDimension(t *testing.T) {
	store := newTestDBStore(t)
	defer store.Close()

	ts := time.Now().UTC()
	err := store.InsertLog(RequestLog{
		ID:        "a1",
		Timestamp: ts,
		Model:     "gpt-5.4",
		Provider:  "openai",
		Route:     "local/codex-file",
		Status:    200,
		Tokens: TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	})
	if err != nil {
		t.Fatalf("insert openai log: %v", err)
	}
	err = store.InsertLog(RequestLog{
		ID:        "a2",
		Timestamp: ts,
		Model:     "gpt-5.4",
		Provider:  "upstream",
		Route:     "UPSTREAM",
		Status:    200,
		Tokens: TokenUsage{
			InputTokens:  20,
			OutputTokens: 8,
		},
	})
	if err != nil {
		t.Fatalf("insert upstream log: %v", err)
	}

	stats, err := store.QueryStats(StatsFilter{})
	if err != nil {
		t.Fatalf("query stats: %v", err)
	}
	if got := len(stats.ByModel); got != 2 {
		t.Fatalf("expected 2 by_model entries, got %d", got)
	}
	if stats.ByModel["openai|gpt-5.4"] == nil {
		t.Fatal("missing openai model entry")
	}
	if stats.ByModel["upstream|gpt-5.4"] == nil {
		t.Fatal("missing upstream model entry")
	}
}
