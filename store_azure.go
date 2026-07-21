package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// azureStore is the Postgres Store impl, backed by db/schema.sql's
// three-table design: jobs (one row per posting, liveness bookkeeping,
// write-once content), scoring_calls (one row per HTTP scoring call, holding
// call-level facts like batch_size and token usage), and scoring_events (one
// row per job scored within a call, FK'd to both). Unlike AWS's DynamoDB impl
// (one item per job, overwritten with the latest score), this preserves
// every scoring event, per CLAUDE.md's hard rule.
type azureStore struct {
	logger *slog.Logger
	pool   *pgxpool.Pool
}

func newAzureStore(logger *slog.Logger, pool *pgxpool.Pool) *azureStore {
	return &azureStore{logger: logger, pool: pool}
}

// scoringCall is one reconstructed ScoreBatch call: every job it scored,
// still grouped together.
type scoringCall struct {
	model       string
	scoredAt    time.Time
	temperature float64
	events      []ScoringEvent
}

// groupByCall reconstructs which ScoringEvents came from the same
// ScoreBatch HTTP call. ScoringEvent carries no call id of its own -
// scorer.go's shape predates the calls/events split - but openaiScorer.
// ScoreBatch stamps every result from one call with the same model.Name and
// a single time.Now() call (see scorer_openai.go), so (Model, ScoredAt) is a
// reliable reconstruction key in practice. Wire an explicit CallID through
// ScoreResult instead if that assumption ever breaks.
func groupByCall(events []ScoringEvent) []scoringCall {
	type key struct {
		model    string
		scoredAt int64
	}
	idx := make(map[key]int)
	var calls []scoringCall
	for _, e := range events {
		k := key{e.Result.Model, e.Result.ScoredAt.UnixNano()}
		i, ok := idx[k]
		if !ok {
			i = len(calls)
			idx[k] = i
			calls = append(calls, scoringCall{model: e.Result.Model, scoredAt: e.Result.ScoredAt, temperature: e.Result.Temperature})
		}
		calls[i].events = append(calls[i].events, e)
	}
	return calls
}

// RecordScores writes one scoring_calls row per reconstructed call plus one
// scoring_events row per job scored in that call, each inside its own short
// transaction - this mirrors AWS's per-item try/log/continue tolerance (a
// bad call is rolled back and logged, the rest of the run proceeds) at the
// finest granularity the new schema allows: scoring_events rows within a
// call share a single call_id FK, so the call is the atomic unit, not the
// individual event.
func (s *azureStore) RecordScores(ctx context.Context, events []ScoringEvent, contributorID, resumeID, configID, instructionsVersion string) error {
	for _, call := range groupByCall(events) {
		if err := s.recordCall(ctx, call, contributorID, resumeID, configID, instructionsVersion); err != nil {
			s.logger.Error("failed to record scoring call", errAttr(err),
				slog.String("model", call.model), slog.Int("batch_size", len(call.events)))
			continue
		}
	}
	return nil
}

func (s *azureStore) recordCall(ctx context.Context, call scoringCall, contributorID, resumeID, configID, instructionsVersion string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wrapErr("begin tx", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// deployment and the itemized token-usage columns are left NULL:
	// ModelConfig and the call's Usage never reach ScoringEvent/ScoreResult
	// today (main.go's `tokens` total from ScoreBatch is local to the
	// scoring loop) - wire those through when that plumbing lands. run_kind
	// is hardcoded for the same reason: no floor-run trigger exists yet in
	// ScoringEvent, real two-tier sampling wiring is future work.
	//
	// contributorID/resumeID/configID/instructionsVersion (CLAUDE.md §10) are
	// constant for the whole run, unlike temperature (call.temperature),
	// which genuinely varies per reconstructed call - so these are passed
	// straight through as literal args rather than threaded through
	// ScoreResult/groupByCall.
	var callID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO scoring_calls (model, batch_size, temperature_sent, run_kind, scored_at, contributor_id, resume_id, config_id, instructions_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING call_id
	`, call.model, len(call.events), call.temperature, "main", call.scoredAt, contributorID, resumeID, configID, instructionsVersion).Scan(&callID)
	if err != nil {
		return wrapErr("insert scoring_calls row", err)
	}

	for _, e := range call.events {
		if err := s.recordEvent(ctx, tx, callID, e); err != nil {
			return err
		}
	}

	return wrapErrIfSet("commit tx", tx.Commit(ctx))
}

func (s *azureStore) recordEvent(ctx context.Context, tx pgx.Tx, callID int64, e ScoringEvent) error {
	payload, err := json.Marshal(e.Job)
	if err != nil {
		return wrapErr("marshal job payload", err)
	}

	stablekey := e.Job.createStableKey()
	compositeKey := e.Job.createCompositeKey()
	postedAt := time.Unix(e.Job.PostedAt, 0)
	descChars := len(e.Job.Description)

	_, err = tx.Exec(ctx, `
		INSERT INTO jobs (composite_key, stablekey, posted_at, source, title, company, url, payload, description_chars, payload_chars)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (composite_key) DO UPDATE SET last_seen = now()
	`, compositeKey, stablekey, postedAt, e.Job.Source, e.Job.Title, e.Job.Company, e.Job.URL,
		string(payload), descChars, len(payload))
	// Job content is write-once above (only last_seen updates on conflict):
	// composite_key already encodes stablekey+posted_at, so a row that
	// matches an existing composite_key is necessarily a re-scrape of the
	// same posting - its content can't have legitimately changed.
	if err != nil {
		return wrapErr("upsert jobs row", err)
	}

	var logprobs any
	if len(e.Result.Logprobs) > 0 {
		logprobs = string(e.Result.Logprobs)
	} // else leave nil -> SQL NULL

	_, err = tx.Exec(ctx, `
		INSERT INTO scoring_events (call_id, composite_key, emitted_score, reasoning, raw, logprobs)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, callID, compositeKey, e.Result.Score, e.Result.Reasoning, string(e.Result.Raw), logprobs)
	// ev_score left NULL: ScoreResult.Score conflates "emitted number" and
	// "logprob EV where supported" into one field (scorer.go's doc comment),
	// so there's no value distinct from emitted_score to store here yet -
	// split ScoreResult before populating this column for real.
	return wrapErrIfSet("insert scoring_events row", err)
}

func (s *azureStore) SeenJobs(ctx context.Context) ([]SeenJob, error) {
	rows, err := s.pool.Query(ctx, `SELECT stablekey, posted_at, last_seen FROM jobs`)
	if err != nil {
		return nil, wrapErr("query seen jobs", err)
	}
	defer rows.Close()

	var out []SeenJob
	for rows.Next() {
		var stablekey string
		var postedAt time.Time
		var lastSeen time.Time
		if err := rows.Scan(&stablekey, &postedAt, &lastSeen); err != nil {
			return nil, wrapErr("scan seen job row", err)
		}
		out = append(out, SeenJob{
			Stablekey: stablekey,
			PostedAt:  postedAt.Unix(),
			// has_applied was dropped from the Azure schema entirely (see
			// db/schema.sql) - CLAUDE.md defers the has_applied/sheet-editing
			// feature and this append-only measurement store never populated
			// it, so there's nothing to read back.
			HasApplied: false,
			LastSeen:   lastSeen,
		})
	}
	return out, wrapErrIfSet("iterate seen jobs", rows.Err())
}

// BumpLastSeen is a single batched UPDATE rather than AWS's per-item loop -
// Postgres supports set-based updates efficiently and this only ever touches
// the liveness bookkeeping row, never scoring_events, so batching carries
// none of RecordScores' fault-isolation concerns.
func (s *azureStore) BumpLastSeen(ctx context.Context, items []SeenJob, now time.Time) error {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, len(items))
	for i, it := range items {
		keys[i] = it.compositeKey()
	}
	_, err := s.pool.Exec(ctx, `UPDATE jobs SET last_seen = $1 WHERE composite_key = ANY($2)`, now, keys)
	return wrapErrIfSet("bump last_seen", err)
}

// DeleteAged is an intentional no-op on Azure: every jobs row that exists
// has already been scored (RecordScores always upserts jobs alongside
// inserting scoring_events), so a literal DELETE would either violate the
// scoring_events FK or (with ON DELETE CASCADE) destroy irreplaceable
// scoring events - violating "preserve every scoring event." AWS's
// DeleteAged exists for DynamoDB cache hygiene, which doesn't apply to an
// append-only measurement store; last_seen remains available for ETL to see
// when a posting vanished.
func (s *azureStore) DeleteAged(ctx context.Context, items []SeenJob) (int, error) {
	if len(items) > 0 {
		s.logger.Info("skipping delete of aged jobs on azure store by design", "count", len(items))
	}
	return 0, nil
}

func (s *azureStore) ExportRows(ctx context.Context) ([]ExportRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (j.stablekey)
			j.stablekey, j.posted_at, j.title, j.company, j.url,
			COALESCE(se.ev_score, se.emitted_score), COALESCE(se.reasoning, ''),
			COALESCE(j.payload->>'location', '')
		FROM jobs j
		JOIN scoring_events se ON se.composite_key = j.composite_key
		JOIN scoring_calls sc ON sc.call_id = se.call_id
		ORDER BY j.stablekey, sc.scored_at DESC
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
			&r.Score, &r.Reasoning, &r.Location); err != nil {
			return nil, wrapErr("scan export row", err)
		}
		r.PostedAt = postedAt.Unix()
		// has_applied no longer exists on Azure's jobs table - see SeenJobs.
		r.HasApplied = false
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
