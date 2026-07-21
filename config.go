package main

import "context"

type ModelConfig struct {
	Name         string
	Deployment   string
	WantLogprobs bool
	TPM          int // tokens/minute quota; handler() refuses to score without a positive value (CLAUDE.md §8)
	RPM          int // requests/minute quota; same requirement
}

// ConfigSource resolves file-shaped app config (sources.json,
// filterKeywords.json, instructions.md) plus the knobs that are
// configuration, never forked code: rescore policy, model list, and
// temperature. Temperature is run-level, not per-model (CLAUDE.md §4.7) - a
// variable compared across models must be held constant across the
// comparison, same reasoning as batch size.
type ConfigSource interface {
	File(ctx context.Context, name string) ([]byte, error)
	Models(ctx context.Context) ([]ModelConfig, error)
	RescoreEveryRun() bool
	Temperature() float32
	BatchSize() int
}
