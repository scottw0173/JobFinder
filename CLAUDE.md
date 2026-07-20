# CLAUDE.md — JobFinder

Context for AI coding sessions. Read this before making changes.

## What this project is

JobFinder is a Go application that:

1. Scrapes job listings from the Greenhouse, Ashby, and Lever APIs.
2. Scores each listing with an AI model using structured outputs, throttled by a token budget.
3. Exports ranked results to Google Sheets.

## Two systems, one codebase

This is a **dual-cloud** app, not a rewrite. Both deployments run simultaneously off the same
binary; they never share state and never collide (separate clouds, separate data stores, each
scrapes the source APIs independently). They do the same activity but are **functionally different
by configuration**, not by forked code:

- **AWS (`main`, live, keep untouched):** cheap mode. Scores each job **once**, caches it in
  DynamoDB until the posting expires. This is the one the human checks daily for jobs that don't
  surface on LinkedIn.
- **Azure (`azure` branch):** data-collection mode. Re-scores jobs across **multiple models** and
  preserves **every scoring event** for future ETL. Runs against the 30-day free credit; goal is to
  maximize the credit window as a data-gathering opportunity.

**Decision (resolved): keep the AWS implementations alongside the Azure ones, behind shared
interfaces.** Two working implementations behind one interface is what proves the abstraction is
real; deleting AWS would collapse the artifact back into a one-way rewrite. Do not delete AWS code
paths. Only the deployment/infra is Azure-exclusive on this branch: **Bicep, no SAM/CloudFormation.**

## Architecture

Business logic is decoupled from concrete cloud clients via four interfaces:

- `Store` — persistence (AWS: DynamoDB, one item/job; Azure: Postgres, one row/event)
- `ConfigSource` — configuration (AWS: SSM/S3; Azure: env/Key Vault)
- `Secrets` — secret retrieval (AWS: SSM; Azure: managed identity via Entra)
- `Scorer` — AI scoring (see below)

Each has an AWS impl and an Azure impl, selected by environment variable. The AWS-coupled logic
being untangled lives across `main.go`, `filter.go`, `helpers.go`, `database.go`, `spreadsheet.go`,
and `logging.go`. The Lambda entrypoint (`lambda.Start`) is replaced with a direct `handler(ctx)`
call so the app runs as a plain binary in a container.

## Operational modes are config, not forks

The two systems' behavioral differences are all runtime configuration:

- **Rescore policy:** `rescoreEveryRun` — false on AWS (skip already-scored jobs), true on Azure.
- **Model list:** the handler loops over a configured list of models. AWS lists one; Azure lists
  several. A loop over a length-1 list behaves identically to the current single-score path.
- **Store shape:** the two `Store` impls differ (upsert-one-item vs append-event); both satisfy the
  same interface, which expresses "record this scoring event," not "set the score field."

## Scoring precision and the data model

The Azure run is a measurement instrument. Two axes matter: **cross-model** (do models agree) and
**temporal drift** (does one model agree with itself over time). Precision rules:

- **Score is a float, never an integer.** Integer 1–10 quantizes real sub-point drift to zero and
  manufactures phantom full-point jumps at boundaries. Use 0–100 or one decimal on 1–10.
- **Don't chase spurious precision.** LLMs heap on round numbers; a directly-emitted second decimal
  is confabulated, not signal. Do not ask the model to type more precision than it has.
- **Prefer a logprob expected value where supported.** Read the distribution over score tokens and
  compute a probability-weighted expected value — a genuinely continuous score grounded in the
  model's distribution, and it moves even at temperature 0 (so drift is measurable under
  deterministic decoding). Logprob support is solid on OpenAI-family models, varies on Foundry
  partner models — it's a per-endpoint check; fall back to the emitted number where unsupported.
- **Rubric-as-vector.** When the rubric lands, score sub-criteria separately so each job yields a
  vector, not a scalar — far more drift signal and diagnostic of *which* dimension moved.
- **Store raw, derive scalars downstream.** Persist the full structured output and the logprob
  distribution verbatim. You can always compute a 0–100 or a rubric-weighted total later; you can
  never recover logprobs or sub-scores you didn't save. "Save every result uniquely" means save it
  *raw*.

**Rubric caveat:** for the temporal-drift axis the rubric can come later. For the cross-model axis
the shared rubric is load-bearing — a 7 from two different models isn't comparable without it. Build
the plumbing now, but do not treat cross-model numbers as valid until the shared rubric is in place.

## Two-tier sampling (data-collection design)

- **Noise floor (periodic, cheap):** take a fixed subset (~20–30 jobs), score them K≈5 times per
  model **at production temperature** (not inflated). Estimates each model's self-variance. This
  floor is the ruler that makes both drift and cross-model differences interpretable.
- **Main dataset (daily):** full ~400 jobs, once per model per day, single score each. Day-over-day
  drift falls out for free because every event is stored with `scored_at`, interpreted against the
  floor.

Do NOT full-rescore the entire set K times daily — that multiplies cost for redundant numbers.

## Models

**Azure Foundry models only** — no Gemini (it's Google/Vertex, not on Azure, would bill off-credit
and complicate the config). The catalog has 170+ models; more than five is fine, but each added
model is another full daily pass against the $200 credit. Pick for **spread** (provider, size,
architecture: a frontier model, a couple of small models, a couple of open-weight families like
Llama/Mistral/DeepSeek/Phi, maybe a reasoning model), not near-duplicate variants. Rough per-token
estimate before committing to cadence: 400 jobs × N models × 30 days.

## Interface + schema sketches (adjust to real repo)

```go
// Scorer scores one job with one model, returning a raw, unreduced result.
type Scorer interface {
    Score(ctx context.Context, job Job, model ModelConfig) (ScoreResult, error)
}

type ModelConfig struct {
    Name         string  // logical name stored in scores.model, e.g. "gpt-4.1-mini"
    Deployment   string  // Azure deployment/endpoint id
    Temperature  float32
    WantLogprobs bool
}

type ScoreResult struct {
    Model     string
    Score     float64            // derived scalar: logprob EV if available, else emitted number
    SubScores map[string]float64 // rubric dimensions; empty until rubric lands
    Raw       json.RawMessage    // full structured output, stored verbatim
    Logprobs  json.RawMessage    // score-token distribution, when available
    ScoredAt  time.Time
}
```

```sql
CREATE TABLE jobs (
    stablekey  TEXT PRIMARY KEY,        -- stable id derived from source + posting
    source     TEXT NOT NULL,           -- greenhouse | ashby | lever
    posted_at  TIMESTAMPTZ,
    title      TEXT, company TEXT, url TEXT,
    payload    JSONB,                   -- raw scraped listing
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE scores (
    id         BIGSERIAL PRIMARY KEY,   -- surrogate key: one row per scoring EVENT
    stablekey  TEXT NOT NULL REFERENCES jobs(stablekey),
    posted_at  TIMESTAMPTZ,             -- part of the logical comparison key
    model      TEXT NOT NULL,
    score      DOUBLE PRECISION,        -- derived scalar
    sub_scores JSONB,                   -- rubric vector; null until rubric lands
    raw        JSONB NOT NULL,          -- full structured model output, verbatim
    logprobs   JSONB,                   -- score-token distribution; null where unsupported
    run_kind   TEXT NOT NULL,           -- 'main' | 'floor'
    scored_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON scores (stablekey, model, scored_at);
-- Logical event identity: (stablekey, posted_at, model, scored_at). Never collapse to one row/pair.
```

## Conventions and hard rules

- **Do not delete AWS implementations** (dual-cloud is the artifact).
- **Preserve every scoring event.** Surrogate PK, one row per event with `scored_at`. Never collapse
  to one row per job/model pair.
- **Store raw.** Persist full structured output + logprobs; derive scalars downstream. Never discard
  raw to keep only a reduced number.
- **Score is float.** Don't round to int; don't emit spurious decimals; prefer logprob EV.
- **Logging is structured JSON to stdout.** Do not reintroduce buffer-logs-to-S3; the platform ships
  logs.
- **Keyless auth on Azure** via managed identity + Entra RBAC — no connection strings or keys in code
  or Bicep. Postgres with managed identity needs Entra auth mode on the server **plus** explicit
  database-principal creation (not plain RBAC).
- **Validate offline:** develop the Azure OpenAI `Scorer` against a local OpenAI-compatible server
  (Ollama / llama.cpp, Phi-4) before Azure is live; validate Bicep with `bicep build`.
- Rescore policy and model list are configuration, never forked code.

## Tech stack

- **Language:** Go
- **AWS (kept, behind interfaces):** Lambda → plain binary, DynamoDB, S3, SSM
- **Azure (target):** Container Apps Jobs, Azure OpenAI / Foundry models, PostgreSQL Flexible Server,
  Entra RBAC, Bicep
- **Local dev:** Docker, docker compose, Ollama or llama.cpp (Phi-4)
- **External:** Greenhouse / Ashby / Lever APIs, Google Sheets

Compute is **Container Apps Jobs** (over Azure Functions: Go isn't first-class there, and the
existing 15-minute runtime exceeds Consumption-plan timeouts).

## Migration sequence (current focus)

~80% is completable before the Azure subscription is active:

1. Replace `lambda.Start` with a direct `handler(ctx)` call.
2. Introduce the four interfaces; keep AWS impls, add Azure impls, select by env var.
3. Replace log-buffer-to-S3 with structured JSON to stdout.
4. Design + run the normalized Postgres schema locally in Docker (`jobs` + `scores`).
5. Containerize: multi-stage Dockerfile + docker compose.
6. Write the Azure OpenAI `Scorer` against the local OpenAI-compatible server.
7. Author the full Bicep infra offline; validate with `bicep build`.

Then Azure day one: pure deploy-and-run to maximize the 30-day credit window.

## Out of scope / deferred

- `has_applied` read-from-sheet. Known latent bug: the sheet is cleared and rewritten each run,
  losing human edits. Deprioritized vs. the Azure learning goals — don't spend effort here unless
  asked.
- The scoring rubric itself is TBD. Build the plumbing (float score, raw capture, sub_scores column)
  now; the rubric content lands later.

## Common commands

> Adjust to match the actual repo layout.

```bash
go build ./...
go test ./...
docker compose up --build
bicep build ./infra/main.bicep
```
