package main

import (
	"context"
	"time"
)

// azureStore is a placeholder until migration step 4 (the normalized Postgres
// schema, jobs + scores, run locally in Docker). handler() already treats
// every Store call as non-fatal (logs and continues), so a stubbed Store lets
// an azure run complete end to end without a live schema to write against.
type azureStore struct{}

func newAzureStore() *azureStore { return &azureStore{} }

func (s *azureStore) RecordScores(ctx context.Context, events []ScoringEvent) error {
	return traceErrorf("azure store not implemented until migration step 4 (Postgres schema)")
}

func (s *azureStore) SeenJobs(ctx context.Context) ([]SeenJob, error) {
	return nil, traceErrorf("azure store not implemented until migration step 4 (Postgres schema)")
}

func (s *azureStore) BumpLastSeen(ctx context.Context, items []SeenJob, now time.Time) error {
	return traceErrorf("azure store not implemented until migration step 4 (Postgres schema)")
}

func (s *azureStore) DeleteAged(ctx context.Context, items []SeenJob) (int, error) {
	return 0, traceErrorf("azure store not implemented until migration step 4 (Postgres schema)")
}

func (s *azureStore) ExportRows(ctx context.Context) ([]ExportRow, error) {
	return nil, traceErrorf("azure store not implemented until migration step 4 (Postgres schema)")
}
