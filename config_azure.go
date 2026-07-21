package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// azureConfigSource is functional now, unlike the other Azure stubs: handler()
// returns fatally if ConfigSource.File errors (via LoadKeywordFilter), so an
// azure run would die at startup without a working implementation. It reads
// config files from a local directory rather than Key Vault/Blob Storage -
// that pairing lands with the Bicep work in migration step 7.
type azureConfigSource struct {
	dir string
}

func newAzureConfigSource() *azureConfigSource {
	dir := os.Getenv("AZURE_CONFIG_DIR")
	if dir == "" {
		dir = "./config"
	}
	return &azureConfigSource{dir: dir}
}

func (c *azureConfigSource) File(ctx context.Context, name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(c.dir, name))
	if err != nil {
		return nil, wrapErr("read azure config file "+name, err)
	}
	return data, nil
}

// defaultAzureModels gives the data-collection run a spread (frontier, small,
// open-weight, reasoning) rather than near-duplicate variants, per CLAUDE.md.
// Real deployment IDs get filled in once the Azure Foundry catalog is wired up.
var defaultAzureModels = []ModelConfig{
	{Name: "gpt-4.1-mini"},
	{Name: "phi-4"},
	{Name: "llama-3.3-70b"},
	{Name: "deepseek-v3"},
}

func (c *azureConfigSource) Models(ctx context.Context) ([]ModelConfig, error) {
	raw := os.Getenv("AZURE_MODELS")
	if raw == "" {
		return defaultAzureModels, nil
	}
	var models []ModelConfig
	if err := json.Unmarshal([]byte(raw), &models); err != nil {
		return nil, wrapErr("parsing AZURE_MODELS", err)
	}
	return models, nil
}

func (c *azureConfigSource) RescoreEveryRun() bool {
	if raw := os.Getenv("AZURE_RESCORE_EVERY_RUN"); raw != "" {
		if v, err := strconv.ParseBool(raw); err == nil {
			return v
		}
	}
	return true
}

// Temperature is a single run-level value applied to every model in this run
// (CLAUDE.md §4.7) - not swept yet, just no longer confounded per-model.
// Default 0 (deterministic) when unset/unparseable.
func (c *azureConfigSource) Temperature() float32 {
	if raw := os.Getenv("AZURE_TEMPERATURE"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil {
			return float32(v)
		}
	}
	return 0
}
