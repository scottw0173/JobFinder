package main

import (
	"context"
	"time"
)

// Store persists job liveness state and scoring events, and answers the
// export query. It expresses "record this scoring event," never "set the
// score field" - AWS upserts one item per job, Azure appends one row per
// event, and both satisfy this same shape.
type Store interface {
	// contributorID/resumeID/configID/instructionsVersion are per-event
	// identity (CLAUDE.md §10) - run-level constants, not per-event data, but
	// threaded through here since they're only known to the caller (handler)
	// and only meaningful to the Azure store; awsStore accepts and ignores
	// them.
	RecordScores(ctx context.Context, events []ScoringEvent, contributorID, resumeID, configID, instructionsVersion string) error
	SeenJobs(ctx context.Context) ([]SeenJob, error)
	BumpLastSeen(ctx context.Context, items []SeenJob, now time.Time) error
	DeleteAged(ctx context.Context, items []SeenJob) (int, error)
	ExportRows(ctx context.Context) ([]ExportRow, error)
}

type ScoringEvent struct {
	Job    Job
	Result ScoreResult
}

type SeenJob struct {
	Stablekey  string
	PostedAt   int64
	HasApplied bool
	LastSeen   time.Time
}

func (s SeenJob) compositeKey() string {
	return compositeKey(s.Stablekey, s.PostedAt)
}

func seenJobKeySet(items []SeenJob) map[string]bool {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.compositeKey()] = true
	}
	return seen
}

type ExportRow struct {
	Stablekey  string
	PostedAt   int64
	Title      string
	Company    string
	Score      float64
	Reasoning  string
	Location   string
	URL        string
	HasApplied bool
}
