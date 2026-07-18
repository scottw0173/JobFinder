package main

import "context"

// azureScorer is a placeholder until migration step 6 (the real Azure OpenAI
// / Foundry Scorer, validated offline against a local OpenAI-compatible
// server before Azure is live). scoreBatchRetry's classify(err) fails fast on
// a non-*statusError, so handler() logs "aborting run, batch failed" and
// moves on rather than looping - a stub is safe here.
type azureScorer struct{}

func newAzureScorer() *azureScorer { return &azureScorer{} }

func (s *azureScorer) ScoreBatch(ctx context.Context, jobs []Job, model ModelConfig) ([]ScoreResult, float64, error) {
	return nil, 0, traceErrorf("azure scorer not implemented until migration step 6 (Azure OpenAI Scorer)")
}
