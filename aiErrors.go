package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type statusError struct{ code int }

func (e *statusError) Error() string {
	return fmt.Sprintf("unexpected status code: %d", e.code)
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

func (a *App) scoreBatchRetry(ctx context.Context, batch []Job) ([]RankedJob, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		res, err := getScores(ctx, a, batch)
		if err == nil {
			return res, nil
		}
		if !classify(err) {
			return []RankedJob{}, err // fail fast
		}
		wait := time.Second << attempt // 1,2,4,8,16s
		if wait > 60*time.Second {
			wait = 60 * time.Second
		}
		wait += time.Duration(rand.Int63n(int64(time.Second)))
		a.Logger.Warn("retrying batch", "attempt", attempt+1, "wait", wait, "err", err)
		select {
		case <-ctx.Done():
			return []RankedJob{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	return []RankedJob{}, fmt.Errorf("batch failed after %d attempts", maxAttempts)
}

/*fails := 0
for _, batch := range batches {
    res, err := a.scoreBatchRetry(ctx, batch)
    if err != nil {
        if fails++; fails >= 3 {
            return fmt.Errorf("aborting run after %d consecutive batch failures: %w", fails, err)
        }
        continue
    }
    fails = 0
    // store res...
}*/
