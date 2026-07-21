package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"
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
// TPM/RPM are deliberately left unset: real values are launch-day VERIFY
// data (CLAUDE.md §9) that don't exist yet, and CLAUDE.md §9 says never
// fabricate an account-dependent number. Consequence: handler()'s throttle
// gate will refuse to score every model here until real TPM/RPM are filled
// in - that's intended, not a bug, until the Azure account exists.
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

// azureSweepStartLayout matches AZURE_SWEEP_START's expected form, e.g. "2026-07-21".
const azureSweepStartLayout = "2006-01-02"

// BatchSize is run-level and swept, not per-model (CLAUDE.md §4.4/§4.5): it's
// computed from the calendar via batchSizeForDay/dayIndex (batchsweep.go)
// rather than read as a plain env value, because the Container Apps Job fires
// on a fixed unattended daily cron - nobody is available to set an env
// correctly every day across the ~30-day window. AZURE_BATCH_SIZE, if set,
// overrides the sweep entirely; that's a manual escape hatch for local
// testing/debugging, not part of the rotation design.
func (c *azureConfigSource) BatchSize() int {
	if raw := os.Getenv("AZURE_BATCH_SIZE"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	raw := os.Getenv("AZURE_SWEEP_START")
	start, err := time.Parse(azureSweepStartLayout, raw)
	if err != nil {
		// Unset/unparseable AZURE_SWEEP_START: degrade to the safest size
		// (smallest batch, least likely to blow a throttle budget) rather
		// than crash the run.
		return batchSizeForDay(0)
	}
	return batchSizeForDay(dayIndex(start, time.Now()))
}
