package main

import "context"

type ModelConfig struct {
	Name         string
	Deployment   string
	Temperature  float32
	WantLogprobs bool
}

// ConfigSource resolves file-shaped app config (sources.json,
// filterKeywords.json, instructions.md) plus the two knobs that are
// configuration, never forked code: rescore policy and model list.
type ConfigSource interface {
	File(ctx context.Context, name string) ([]byte, error)
	Models(ctx context.Context) ([]ModelConfig, error)
	RescoreEveryRun() bool
}
