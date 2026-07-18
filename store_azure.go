package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// azureStore is the Postgres Store impl: one jobs row per stablekey
// (liveness bookkeeping, upserted), one scores row per scoring event
// (appended, never upserted) - see db/schema.sql. Unlike AWS's DynamoDB impl
// (one item per job, overwritten with the latest score), this preserves
// every scoring event, per CLAUDE.md's hard rule.
type azureStore struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

func newAzureStore(logger *slog.Logger, pool *pgxpool.Pool) *azureStore {
	return &azureStore{logger: logger, pool: pool}
}

// RecordScores upserts the parent jobs row and appends a scores row for each
// event, one short transaction per event - this mirrors AWS's per-item
// try/log/continue tolerance (a bad event is rolled back and logged, the
// rest of the batch proceeds) at the finest granularity Postgres offers
// without SAVEPOINTs, and is cheap here since RecordScores runs once after
// the (much slower) scoring loop has already completed.
func (s *azureStore) RecordScores(ctx context.Context, events []ScoringEvent) error {
	for _, e := range events {
		if err := s.recordOne(ctx, e); err != nil {
			s.logger.Error("failed to record scoring event", errAttr(err),
				slog.String("stablekey", e.Job.createStableKey()), slog.String("model", e.Result.Model))
			continue
		}
	}
	return nil
}

func (s *azureStore) recordOne(ctx context.Context, e ScoringEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wrapErr("begin tx", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	payload, err := json.Marshal(e.Job)
	if err != nil {
		return wrapErr("marshal job payload", err)
	}

	stablekey := e.Job.createStableKey()
	postedAt := time.Unix(e.Job.PostedAt, 0)

	_, err = tx.Exec(ctx, `
		INSERT INTO jobs (stablekey, source, posted_at, title, company, url, payload)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (stablekey) DO UPDATE SET
			source = EXCLUDED.source,
			posted_at = EXCLUDED.posted_at,
			title = EXCLUDED.title,
			company = EXCLUDED.company,
			url = EXCLUDED.url,
			payload = EXCLUDED.payload,
			last_seen = now()
		-- first_seen and has_applied deliberately excluded: preserve original
		-- first_seen, never clobber a human-set has_applied flag on rescore.
	`, stablekey, e.Job.Source, postedAt, e.Job.Title, e.Job.Company, e.Job.URL, string(payload))
	if err != nil {
		return wrapErr("upsert jobs row", err)
	}

	subScores, err := json.Marshal(e.Result.SubScores)
	if err != nil {
		return wrapErr("marshal sub_scores", err)
	}
	var logprobs any
	if len(e.Result.Logprobs) > 0 {
		logprobs = string(e.Result.Logprobs)
	} // else leave nil -> SQL NULL

	_, err = tx.Exec(ctx, `
		INSERT INTO scores (stablekey, posted_at, model, score, sub_scores, raw, logprobs, reasoning, run_kind, scored_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, stablekey, postedAt, e.Result.Model, e.Result.Score, string(subScores),
		string(e.Result.Raw), logprobs, e.Result.Reasoning,
		"main", // hardcoded: no floor-run trigger exists yet in ScoringEvent;
		// real two-tier sampling wiring is future work, not step 4.
		e.Result.ScoredAt)
	if err != nil {
		return wrapErr("insert scores row", err)
	}

	return wrapErrIfSet("commit tx", tx.Commit(ctx))
}

func (s *azureStore) SeenJobs(ctx context.Context) ([]SeenJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT stablekey, posted_at, has_applied, last_seen FROM jobs`)
	if err != nil {
		return nil, wrapErr("query seen jobs", err)
	}
	defer rows.Close()

	var out []SeenJob
	for rows.Next() {
		var stablekey string
		var postedAt time.Time
		var hasApplied bool
		var lastSeen time.Time
		if err := rows.Scan(&stablekey, &postedAt, &hasApplied, &lastSeen); err != nil {
			return nil, wrapErr("scan seen job row", err)
		}
		out = append(out, SeenJob{
			Stablekey:  stablekey,
			PostedAt:   postedAt.Unix(),
			HasApplied: hasApplied,
			LastSeen:   lastSeen,
		})
	}
	return out, wrapErrIfSet("iterate seen jobs", rows.Err())
}

// BumpLastSeen is a single batched UPDATE rather than AWS's per-item loop -
// Postgres supports set-based updates efficiently and this only ever touches
// the liveness bookkeeping row, never scores, so batching carries none of
// RecordScores' fault-isolation concerns.
func (s *azureStore) BumpLastSeen(ctx context.Context, items []SeenJob, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = it.Stablekey
	}
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET last_seen = $1 WHERE stablekey = ANY($2)`, now, keys)
	return wrapErrIfSet("bump last_seen", err)
}

// DeleteAged is an intentional no-op on Azure: every jobs row that exists
// has already been scored (RecordScores always upserts jobs alongside
// inserting scores), so a literal DELETE would either violate the scores FK
// or (with ON DELETE CASCADE) destroy irreplaceable scoring events -
// violating "preserve every scoring event." AWS's DeleteAged exists for
// DynamoDB cache hygiene, which doesn't apply to an append-only measurement
// store; last_seen remains available for ETL to see when a posting vanished.
func (s *azureStore) DeleteAged(ctx context.Context, items []SeenJob) (int, error) {
	if len(items) > 0 {
		s.logger.Info("skipping delete of aged jobs on azure store by design", "count", len(items))
	}
	return 0, nil
}

func (s *azureStore) ExportRows(ctx context.Context) ([]ExportRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (j.stablekey)
			j.stablekey, j.posted_at, j.title, j.company, j.url, j.has_applied,
			s.score, COALESCE(s.reasoning, ''), COALESCE(j.payload->>'location', '')
		FROM jobs j
		JOIN scores s ON s.stablekey = j.stablekey
		ORDER BY j.stablekey, s.scored_at DESC
	`)
	if err != nil {
		return nil, wrapErr("query export rows", err)
	}
	defer rows.Close()

	var out []ExportRow
	for rows.Next() {
		var r ExportRow
		var postedAt time.Time
		if err := rows.Scan(&r.Stablekey, &postedAt, &r.Title, &r.Company, &r.URL,
			&r.HasApplied, &r.Score, &r.Reasoning, &r.Location); err != nil {
			return nil, wrapErr("scan export row", err)
		}
		r.PostedAt = postedAt.Unix()
		out = append(out, r)
	}
	return out, wrapErrIfSet("iterate export rows", rows.Err())
}

func wrapErrIfSet(msg string, err error) error {
	if err == nil {
		return nil
	}
	return wrapErr(msg, err)
}
