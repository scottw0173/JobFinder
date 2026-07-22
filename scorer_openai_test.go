package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"
)

// testInstructions is deliberately short (unlike the real config/instructions.md,
// which holds personal candidate data and isn't guaranteed to exist in a clean
// checkout) - this test verifies the scorer's wire protocol and JobKey
// correlation, not rubric quality.
const testInstructions = `You are scoring jobs for fit. For each job, return its
"key" value unchanged, a score, and a one-sentence reasoning.`

func newTestOpenAIScorer(t *testing.T) (*openaiScorer, string) {
	t.Helper()
	baseURL := os.Getenv("AZURE_OPENAI_TEST_ENDPOINT")
	if baseURL == "" {
		t.Skip("AZURE_OPENAI_TEST_ENDPOINT not set; skipping (requires docker compose up -d ollama + a pulled model)")
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := &http.Client{Timeout: 3 * time.Minute}
	return newOpenAIScorer(logger, client, "", nil, []byte(testInstructions)), baseURL
}

func TestOpenAIScorerScoreBatchCorrelatesByKey(t *testing.T) {
	s, baseURL := newTestOpenAIScorer(t)
	ctx := context.Background()

	model := ModelConfig{Name: os.Getenv("AZURE_OPENAI_TEST_MODEL"), BaseURL: baseURL, Protocol: "openai"}
	if model.Name == "" {
		model.Name = "phi4-mini"
	}

	jobs := []Job{
		{Key: "acmeSWERemote1000", Title: "Software Engineer", Company: "Acme", Location: "Remote", Description: "Build backend services in Go."},
		{Key: "betaPMNYC2000", Title: "Product Manager", Company: "Beta", Location: "NYC", Description: "Own the roadmap for a small B2B product."},
	}

	results, tokens, err := s.ScoreBatch(ctx, jobs, model, 0.7)
	if err != nil {
		t.Fatalf("ScoreBatch: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	if tokens <= 0 {
		t.Errorf("expected nonzero token usage, got %v", tokens)
	}

	validKeys := map[string]bool{"acmeSWERemote1000": true, "betaPMNYC2000": true}
	for _, r := range results {
		if !validKeys[r.JobKey] {
			t.Errorf("result JobKey %q does not match any input job key", r.JobKey)
		}
		if len(r.Raw) == 0 {
			t.Errorf("result for %q has empty Raw - violates the store-raw hard rule (scores.raw is NOT NULL)", r.JobKey)
		}
		if r.Score < 0 || r.Score > 100 {
			t.Errorf("result for %q has out-of-range score %v (expected 0-100)", r.JobKey, r.Score)
		}
		if r.Model != model.Name {
			t.Errorf("result Model = %q, want %q", r.Model, model.Name)
		}
	}
}

func TestOpenAIScorerAuthHeaderValue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx := context.Background()

	tests := []struct {
		name      string
		apiKey    string
		cred      *fakeTokenCredential
		authScope string
		want      string
		wantErr   bool
	}{
		{name: "no auth configured", want: ""},
		{name: "static key only", apiKey: "sk-local", want: "Bearer sk-local"},
		{
			name:      "AuthScope with working credential prefers token over static key",
			apiKey:    "sk-local",
			cred:      &fakeTokenCredential{token: "fake-aad-token"},
			authScope: "https://cognitiveservices.azure.com/.default",
			want:      "Bearer fake-aad-token",
		},
		{
			name:      "AuthScope with failing credential errors",
			cred:      &fakeTokenCredential{err: errTestCredential},
			authScope: "https://cognitiveservices.azure.com/.default",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &openaiScorer{logger: logger, apiKey: tt.apiKey}
			if tt.cred != nil {
				s.cred = tt.cred
			}
			got, err := s.authHeaderValue(ctx, ModelConfig{AuthScope: tt.authScope})
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("authHeaderValue: %v", err)
			}
			if got != tt.want {
				t.Errorf("authHeaderValue() = %q, want %q", got, tt.want)
			}
		})
	}
}
