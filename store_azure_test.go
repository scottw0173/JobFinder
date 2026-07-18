package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestAzureStore(t *testing.T) *azureStore {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set; skipping (requires docker compose up -d)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting to test postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return newAzureStore(slog.New(slog.NewTextHandler(os.Stderr, nil)), pool)
}

func TestAzureStoreRecordScoresPreservesEveryEvent(t *testing.T) {
	s := newTestAzureStore(t)
	ctx := context.Background()

	job := Job{Company: "Acme", Title: "SWE", Location: "Remote", Source: "greenhouse", PostedAt: time.Now().Unix()}
	events := []ScoringEvent{
		{Job: job, Result: ScoreResult{Model: "gpt-4.1-mini", Score: 71.2, Reasoning: "r1", Raw: json.RawMessage(`{"a":1}`), ScoredAt: time.Now()}},
		{Job: job, Result: ScoreResult{Model: "phi-4", Score: 65.5, Reasoning: "r2", Raw: json.RawMessage(`{"a":2}`), ScoredAt: time.Now().Add(time.Minute)}},
	}
	if err := s.RecordScores(ctx, events); err != nil {
		t.Fatalf("RecordScores: %v", err)
	}

	// Two events, same stablekey, two different models -> must be two rows,
	// never collapsed - assert via ExportRows picking the most recent.
	rows, err := s.ExportRows(ctx)
	if err != nil {
		t.Fatalf("ExportRows: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Stablekey == job.createStableKey() {
			found = true
			if r.Reasoning != "r2" {
				t.Fatalf("expected most-recent event (phi-4/r2), got reasoning=%q", r.Reasoning)
			}
		}
	}
	if !found {
		t.Fatal("expected exported row for test job")
	}
}

func TestAzureStoreSeenJobsBumpAndDeleteAged(t *testing.T) {
	s := newTestAzureStore(t)
	ctx := context.Background()

	job := Job{Company: "Beta", Title: "PM", Location: "NYC", Source: "lever", PostedAt: time.Now().Unix()}
	events := []ScoringEvent{{Job: job, Result: ScoreResult{Model: "m1", Score: 50, Raw: json.RawMessage(`{}`), ScoredAt: time.Now()}}}
	if err := s.RecordScores(ctx, events); err != nil {
		t.Fatalf("RecordScores: %v", err)
	}

	seen, err := s.SeenJobs(ctx)
	if err != nil {
		t.Fatalf("SeenJobs: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("expected at least one seen job")
	}

	now := time.Now()
	if err := s.BumpLastSeen(ctx, seen, now); err != nil {
		t.Fatalf("BumpLastSeen: %v", err)
	}

	n, err := s.DeleteAged(ctx, seen)
	if err != nil {
		t.Fatalf("DeleteAged: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected DeleteAged to be a no-op on azure store, got %d deleted", n)
	}
}
