package main

import (
	"context"
	"encoding/json"
	"time"
)

// Scorer scores a batch of jobs against one model in a single logical call -
// this matches the real Gemini code (one HTTP call scores up to 5 jobs,
// sharing a token-throttle budget), not a one-job-at-a-time shape. Results
// correlate back to input jobs via ScoreResult.JobKey rather than by
// position, since a provider may drop or reorder a job in its response.
// tokens is the total consumed by this call; return 0 if the provider
// doesn't report usage.
type Scorer interface {
	ScoreBatch(ctx context.Context, jobs []Job, model ModelConfig) ([]ScoreResult, float64, error)
}

type ScoreResult struct {
	JobKey    string
	Model     string
	Score     float64 // derived scalar: logprob EV where supported, else emitted number
	Reasoning string
	SubScores map[string]float64 // rubric dimensions; empty until the rubric lands
	Raw       json.RawMessage    // full structured output, stored verbatim
	Logprobs  json.RawMessage    // score-token distribution; nil where unsupported
	ScoredAt  time.Time
}

// zipScoreEvents joins jobs with their scoring results by JobKey. A job with
// no matching result (the provider dropped it) is logged and skipped rather
// than producing a zero-value event.
func zipScoreEvents(a *App, jobs []Job, results []ScoreResult) []ScoringEvent {
	byKey := make(map[string]ScoreResult, len(results))
	for _, r := range results {
		byKey[r.JobKey] = r
	}
	events := make([]ScoringEvent, 0, len(jobs))
	for _, j := range jobs {
		r, ok := byKey[j.Key]
		if !ok {
			a.Logger.Warn("no score returned for job", "key", j.Key)
			continue
		}
		events = append(events, ScoringEvent{Job: j, Result: r})
	}
	return events
}
