package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type tpmThrottle struct {
	budget  float64
	window  time.Duration
	history []sample
}

type sample struct {
	at     time.Time
	tokens float64
}

type statusError struct{ code int }

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected status code: %d", e.code)
}

func (t *tpmThrottle) reserve(ctx context.Context, est float64) error {
	for {
		deadline := time.Now().Add(-t.window)
		sum, keep := 0.0, t.history[:0]
		for _, s := range t.history {
			if s.at.After(deadline) {
				keep = append(keep, s)
				sum += s.tokens
			}
		}
		t.history = keep
		if sum+est <= t.budget {
			return nil
		}
		if len(t.history) == 0 {
			app.Logger.Warn("batch estimate exceeds full budget", "est", est)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Until(t.history[0].at.Add(t.window))):
			continue
		}
	}
}

func (t *tpmThrottle) record(tokens float64) {
	t.history = append(t.history, sample{
		at:     time.Now(),
		tokens: tokens,
	})
}

func classify(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.code {
	case 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func (a *App) scoreBatchRetry(ctx context.Context, batch []Job) ([]RankedJob, float64, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		res, tokens, err := getScores(ctx, a, batch)
		if err == nil {
			return res, tokens, nil
		}
		if !classify(err) {
			return []RankedJob{}, 0, err // fail fast
		}
		wait := time.Second << attempt // 1,2,4,8,16s
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		wait += time.Duration(rand.Int63n(int64(time.Second)))
		a.Logger.Warn("retrying batch", "attempt", attempt+1, "wait", wait, "err", err)
		select {
		case <-ctx.Done():
			return []RankedJob{}, 0, ctx.Err()
		case <-time.After(wait):
		}
	}
	return []RankedJob{}, 0, traceErrorf("batch failed after %d attempts", maxAttempts)
}
