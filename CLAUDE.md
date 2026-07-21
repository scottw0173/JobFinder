# CLAUDE.md — JobFinder (Azure branch / data-gathering instrument)

> **How to use this file.** This is a reasoning-first spec, not a changelog. Each
> decision below states *why* it is the way it is. The "why" is load-bearing:
> several of these choices look like obvious targets for "helpful" simplification
> (e.g. "just batch the calls," "just store the cost," "just use one price"), and
> undoing them silently corrupts data on a **non-repeatable 30-day window**. If a
> change seems to contradict a decision here, surface the contradiction and the
> reasoning before acting — do not optimize it away.

---

## 1. Frame: what this project *is now*

This branch began as an **AWS→Azure migration** (make the app cloud-agnostic). That
goal is essentially done. The project has since crossed from a **build goal** to a
**use goal**: the Azure implementation is now a **measurement instrument** whose
purpose is to collect data comparing AI models on job-fit scoring.

The consequence: most design questions are no longer about *portability/equivalence*
(does Azure do what AWS did) but about *measurement validity* (is what I capture
attributable, are my comparisons confounded, does an operating choice destroy a
quantity I need). When in doubt, optimize for **measurement validity and data
recoverability**, not for operational tidiness or cost-per-run.

### Goal separation (why this is a separate repo)
- **JobFinder-AWS (original, solo-built):** demonstrates independent engineering.
- **JobFinder-Azure (this, AI-assisted):** demonstrates AI-tool fluency *and* a
  multi-model evaluation study. Kept in a **separate repository** deliberately —
  merging blurs two distinct portfolio signals. This CLAUDE.md belongs to the
  Azure project only; it should not bleed into the solo repo.

---

## 2. Current state (build frontier)

**Done (migration frame):**
- `handler(ctx)` runs as a plain binary; `lambda.Start` only when
  `AWS_LAMBDA_RUNTIME_API` is set. Provider chosen by `CLOUD_PROVIDER` env.
- Four interfaces — `Store`, `ConfigSource`, `Secrets`, `Scorer` — with AWS and
  Azure implementations, wired in `wireAWS` / `wireAzure`.
- Structured logging to stdout.
- Postgres `Store` fully implemented (`store_azure.go`) against `db/schema.sql`
  (append-per-event `scores`, upsert-per-job `jobs`).
- Multi-model scoring loop in `handler()`.
- OpenAI-compatible scorer (`scorer_azure.go`) incl. best-effort logprob EV;
  runs against local Ollama today.
- Container stack (multi-stage Dockerfile, docker-compose with postgres+ollama+app).
- Bicep infra (`infra/`) compiles clean; provisions identity, ACR, Postgres
  (AAD-only), Foundry/OpenAI account + model deployments, storage, key vault,
  RBAC, Container Apps Job.

**Not done (the frontier — mostly *use*-frame work):**
- **Keyless managed-identity auth** (Postgres + Azure OpenAI). This is the last
  migration-frame task. See §9.
- **Config/secrets delivery** to the deployed container. See §9.
- All the **measurement-instrument** changes in §§4–8 (scorer protocol refactor,
  itemized token capture, per-model throttle, batch-size knob, schema additions).

> A fresh session catching up should read code in **execution order**
> (`main` → `wireAzure` → `handler` → `collect`/`filter`/scorer/store), not file
> order, and treat "why this instead of the obvious simpler thing" as the
> comprehension test.

---

## 3. Architecture seams

`App` holds `Store`, `ConfigSource`, `Secrets`, `Scorer` behind interfaces so the
same `handler()` runs on either cloud. **Cloud selection lives in `wireAWS`/
`wireAzure`.** The scoring layer must *not* care about cloud — it cares about
**wire protocol** (see §7). This is the key seam correction for the use frame: the
axis that varies is no longer the cloud, it's the provider protocol.

---

## 4. Measurement decisions (the core of the instrument)

### 4.1 Store every scoring event; never overwrite
`scores` **appends one row per (job × model × run)**, surrogate PK, with
`scored_at`. AWS's DynamoDB store overwrites one item per job; the Azure store must
not. Rationale: drift, consistency, and cost analyses all need the full history of
scoring events. Collapsing to one-row-per-job destroys the second moment
(variance) that the consistency question depends on. `scored_at` is also
load-bearing for cost derivation, which is downstream/out-of-scope (§4.3).

### 4.2 Capture token facts maximally itemized — they are perishable
The API reports token counts **once, in the response**, and they are **not
reconstructable later**. Capture, per event, as finely as each provider reports:
`input_uncached`, `cache_read`, `cache_write`, `output` (visible), `reasoning`
(hidden CoT), plus **the raw `usage` JSON blob verbatim** as backstop. Null means
"provider didn't itemize it," which is itself information. Store `batch_size` and
all submission conditions alongside (§4.5) — granular counts are uninterpretable
without the conditions that produced them.

**Do not** compute or store cost/ratios/apportionment at capture time. Store the
atomic facts; derive summaries later. *Rule: store what's perishable and atomic;
derive what's reconstructable.*

### 4.3 Do not compute or store cost — capture tokens instead
The program must **never calculate or persist cost**. Cost = tokens × prices;
prices are mutable (they change, or a rate is later found mis-read), so any stored
cost is wrong the moment they do. Tokens are immutable facts. So the app's only job
here is to **capture the full itemized token facts** (§4.2) and store nothing
derived from them. Cost is computed later, **outside this program**, from the
captured tokens and a separate price table — not app code, out of scope for what
you build here.


### 4.4 Batch size is **swept**, not fixed; batch=1 is one condition among equals
There is **no default batch size**. Batch size is a variable, rotated across
`[1,2,3,5,10]` (mechanics in §4.5). Each size is a legitimate condition serving a
real question; none is over-weighted.

Batch=1 has one property the others lack: **per-job output/reasoning tokens are
measurable only there.** In a batched call the API returns *one* `completion_tokens`
for the whole batch; the per-job split was never emitted, so any apportionment
(input-proportional, equal, etc.) is a *fabrication* — and input-proportional
apportionment is specifically *biased*, because reasoning tracks job *difficulty*,
not JD *length*, so the error correlates with the very signal being studied. So
per-job token cost must be **measured at batch=1, not assumed from batched runs**.

But **necessity is not importance.** Batch=1 being the only place a quantity is
measurable does *not* make it the primary regime or earn it extra sampling. The
batch-size→consistency and batch-size→cost curves are equally central, and they
need the *other* sizes with equal weight to have any shape; amortization/cache
economics are only observable *by* batching. Over-sampling batch=1 would starve the
curves to over-power a variable already adequately covered. Treat all five sizes as
co-equal conditions (§4.5, equal rotation).

### 4.5 Batch size is **run-level and constant within a comparison**
Batch size changes what's measured (anchoring, attention dilution, per-job
reasoning compression), so it is a **treatment**, not a nuisance. Any two things
compared must share a batch size, else model-vs-batch is unidentifiable. Therefore
batch size is a **single per-run env value** (`BATCH_SIZE`), inherited by all
models in that run — *not* a per-model field.

**Sweep design:** rotate batch size across `[1,2,3,5,10]` **daily**, not in weekly
blocks — interleaving decorrelates batch size from calendar/feed drift. **Equal
allocation** — each size gets the same number of days (~6 over a clean ~30-day
window); no size is over-sampled (§4.5). Use a **max-spread, cycle-rotated
permutation** (e.g. `1,10,2,5,3`, re-permuted each cycle) so adjacent days span the
range *and* each size walks across weekdays by construction (near-Latin-square),
rather than a numeric cycle (leaves fixed day-of-week pairings) or pure random (can
hand a bad single realization on a one-shot window). Rescore the **fixed panel**
under every batch size so batch size varies against constant jobs.


### 4.6 Logprob EV over integer scoring
Store two separate columns per scoring event; never collapse them into one:
- **`emitted_score`** — the number the model actually returned. Always populated.
  This is a raw fact from the response.
- **`ev_score`** — the probability-weighted expected value over the score token
  (`bestEffortScoreEV`), computed from logprobs. **Nullable:** `NULL` whenever
  logprobs are absent or unusable and the EV path didn't fire.

Why both, separate: they are different facts (one returned, one derived from it),
and the EV path fires **unevenly across the catalog** (providers differ in whether
they return usable logprobs). A single blended "score" column would silently mix
two kinds of measurement in a way that correlates with model — and would erase, per
row, which one you got. Keeping them apart preserves provenance and lets "how much
does EV differ from emitted, and for which models" be answerable.

- The **`NULL` on `ev_score` is itself the provenance signal** — it marks exactly
  the rows where logprobs weren't usable. No separate flag needed.
- EV is **best-effort, never required.** A model returning no usable logprobs still
  produces a valid row: real `emitted_score`, `NULL` `ev_score`.
- **Known gap:** multi-token numerals (a score split across tokens) aren't handled
  by the EV path → `ev_score` is `NULL` there too. Don't "fix" this by guessing;
  leave the fallback.
- Requesting logprobs is gated by `WantLogprobs` on `ModelConfig` (§6).


### 4.7 Temperature is a run-level variable, not per-model
Temperature is **run-level** (e.g. a `TEMPERATURE` env value), constant across all
models within a run — **not** a `ModelConfig` field. Same rule as batch size (§4.5):
a variable compared across models must be held constant across the comparison, or
model-vs-temperature is confounded and unidentifiable. If temperature is swept, it
is applied **uniformly to every model on a given run** and varied *across* runs, not
between models within one run.

Code requirements:
- Read one temperature per run; apply to every model's scoring request.
- **Record the temperature actually used, per scoring event** (a column on the
  row) — so it's a groupable condition, and so any provider-side clamping/reinterp
  is visible (providers differ in how they honor a given value; capture what was
  sent, don't assume it was obeyed).

---

## 5. Capture grain (analysis is out of scope for this program)
This program **collects data only**. It does not compute consistency, correlations,
cost, aggregates, or any analysis — those happen later, off-machine, against the
collected rows. Do **not** add stdev/correlation/cost/summary logic to the app.

The program's sole obligation is to **capture each scoring event at a fine enough
grain that downstream analysis is possible** — the raw atomic facts (§4.1–4.2,
§4.6) plus **every condition under which the event was produced**, one un-aggregated
row per event. Capture conditions maximally: any variable that could differ between
events and isn't recorded becomes an unrecoverable confound on a non-repeatable run.
If a fact is captured per-event and un-aggregated, the analysis can be done later;
if it's aggregated or dropped at capture, it can't. That asymmetry governs what the
program stores.

---

## 6. ModelConfig vs run-level params

**Per-model (`ModelConfig`)** — instance/protocol data:
- `Name` (model string) 
- `Deployment` (Azure OpenAI deployment; distinct from Name)
- `BaseURL` (endpoint)
- `Protocol` (which scorer to use — see §7)
- `AuthScope` / credential kind (differs per endpoint: Cognitive Services scope,
  Gemini key, OpenAI-compatible key)
- `TPM` 
- `RPM` (per-deployment rate limits)
- `WantLogprobs`

**Explicitly NOT in ModelConfig:**
- **Per-token prices** → not stored/computed by the app at all (§4.3).
- **Temperature** §4.7
- **Batch size, RescoreEveryRun** → **run-level** (§4.5), constant across models
  within a run.

---

## 7. Scorer seam: split by **protocol**, not cloud, not provider-name

`azureScorer` is misnamed — it's really an **OpenAI-compatible** scorer (Ollama
today, Azure OpenAI/Foundry later; only `baseURL`/auth/`deployment` change). Rename
accordingly. The seam is **one implementation per distinct wire protocol**, chosen
per model via `ModelConfig.Protocol`:
- `openaiScorer` — the OpenAI `/chat/completions` dialect. Covers OpenAI, most
  Foundry MaaS, Ollama, and likely DeepSeek (it exposes an OpenAI-compatible API).
- `geminiScorer` — genuinely different shape (AWS incumbent).
- Additional protocol scorers **only** if a Foundry model exposes a non-OpenAI
  surface (verify — see §9).

**Consequence:** adding an OpenAI-compatible model is **config only** (a new
ModelConfig row). Only a genuinely new protocol requires code. `handler()` routes
to a scorer by the model's `Protocol` tag; the scorer encodes protocol logic,
ModelConfig supplies instance data.

> Note the real-Azure-OpenAI request shape may differ from the OpenAI-compatible
> one (`/openai/deployments/{deployment}/chat/completions?api-version=…` and an
> `api-key` header vs. Bearer). Reconcile which surface each model uses, and wire
> `Deployment` accordingly, when keyless auth lands.

---

## 8. Throttle logic

Current throttle is Gemini-shaped and hardcoded — will 429 on a mixed panel. Make
it a **function of `BATCH_SIZE` and the per-model `TPM`/`RPM`**, derated to **75%**:
- Token budget = `floor(0.75 * TPM)` per window.
- Request interval = `ceil(60 / (0.75 * RPM))` seconds (ceil the *interval* — flooring
  it rounds you *faster*, toward the limit; ceil keeps the 75% margin protective).
- **Effective throttle = whichever limit binds at the current batch size**: small
  batches are RPM-bound (many requests), large batches TPM-bound (many tokens/req).
  Apply both, derated; the tighter one governs. A token-only throttle will breach
  RPM at batch=1.
- Throttle is **per-model** (each has its own TPM/RPM), recomputed when `BATCH_SIZE`
  changes.


---

## 9. Deferred work & the "no Azure account yet" boundary

**No Azure subscription exists yet — by design.** Everything account-dependent is
being prepared offline first. Do **not** build or "fix" anything that requires a
live Azure resource to exist or be tested against; writing speculative integration
code against resources that aren't there produces untestable guesses that fail
silently once credits are burning. When a task below is account-dependent, the
correct action is to build only the offline-verifiable *shape* and stop — not to
complete and assume it works.

### Buildable/testable offline now
- **Keyless-auth code *structure*** — the shapes, not the live behavior:
  - **Postgres:** a `pgxpool.Config.BeforeConnect` hook that fetches an Entra token
    via `azidentity` and uses it as the connection password (Postgres will have
    `passwordAuth: Disabled` — no static password). The hook structure, refresh
    logic, and wiring are writable and unit-testable now against a *fake* token
    provider.
  - **Azure OpenAI:** replace the scorer's static Bearer key with a token from
    `azidentity` (the account will be keyless, `disableLocalAuth: true`). Again:
    structure and wiring now, against a fake credential.

### Account-dependent — DO NOT build/assume until the subscription is live
- **Token scopes are UNVERIFIED.** The scopes for Postgres-AAD and Cognitive
  Services are noted from memory and must be confirmed against live Azure before
  they're trusted. Do not hardcode them as known-correct; leave them clearly
  marked as to-confirm.
- **That any of it actually authenticates** cannot be tested offline —
  `ManagedIdentityCredential` needs a real Azure identity. "Compiles / unit-tests
  with a fake provider" ≠ "works." Do not mark keyless auth done on offline tests.
- **Config/secrets delivery** (how the deployed Job gets `instructions.md`,
  filter/sources config, and Google Sheets creds) is **deferred entirely.** The
  mechanism (Blob-backed ConfigSource vs. baked-in vs. Key Vault) is undecided and
  depends on resources that don't exist. Do not implement it yet. Local dev uses
  `AZURE_CONFIG_DIR` on disk; that is the *only* config path that should exist
  until the delivery mechanism is decided.

### Cap offline-test realism — "buildable" ≠ "worth simulating"
There are three tiers of work, and the middle one is a trap:
1. **Pure logic, no external dependency** (schema, throttle math, batch rotation,
   JSON parsing, ModelConfig plumbing) — build *and* genuinely unit-test now. The
   test means something.
2. **Talks to an external service** (scorers, auth hooks) — build now, prove the
   *shape* against the **cheapest possible fake** (a hardcoded OpenAI-compatible
   JSON response, a fake token provider), then **stop**. Do **not** build
   infrastructure to make the fake realistic.
3. **Behavior only exists live** (real auth handshake, real endpoint shapes,
   quotas) — do not build/assume until the account exists.

The explicit cap, from a real prior mistake: a past session stood up a full local
Ollama/Phi-4 serving stack so the scorer could be "tested" offline. That validated
*that Ollama works*, not *that the scorer is correct* — a stub returning canned JSON
would have proven the wiring for a fraction of the effort. **Do not build local
models, mock servers, or realistic simulators to make offline tests more lifelike.**
A trivial stub is not only cheaper but *more honest*: a realistic local harness
breeds false confidence ("it worked against Ollama!") that masks real integration
gaps (e.g. Azure OpenAI's actual request shape differing from Ollama's — see §7).
The real endpoint is the real test, and it arrives on launch day regardless.

---

## 10. Multi-contributor optionality (design for it now; recruit later)

The strongest version of this study is **multi-subject**: other people running
their own months with **different resumes/instructions/panels**. That's the only
way to ask whether fit-scoring quality is a property of the *model* or of the
*person-model match* — turning "model X agrees with *me*" into a general claim. It
also sidesteps the single-window sample-size limit.

Two cheap-now / expensive-later design consequences — build them in even if you run
only your own month:
- **Per-event contributor/config identity:** contributor id, resume/config id,
  instructions version, on every row. Without it, person-effects and model-effects
  are inseparable — the whole point.
- **Turnkey redeployability:** the Bicep + container + config must stand up from
  scratch on a *stranger's* subscription with *their* inputs. This is the same bar
  as "day-one deploy-and-run," and the keyless-auth/config-delivery work above is
  exactly what makes it possible. "A distributable measurement harness multiple
  people deployed to their own cloud accounts" is a far stronger portfolio claim
  than a personal script.

---

## 11. Working agreements (leash)
- **Understand before changing.** Explore/read code before executing edits; prefer
  plan-mode (`defaultMode: "plan"` in `.claude/settings.json`).
- **Don't undo §4–§8 decisions to simplify.** They look like optimization targets;
  they're validity constraints on a window that can't be rerun. Surface, don't
  silently "fix."
- **Raw SQL via `pgx/v5` / `pgxpool`**, no ORM (matches existing style;
  `lib/pq` avoided — maintenance mode).
- **No secrets in this file or any committed file.** Reasoning and structure only.
- **README is deferred** until the program is complete and the repo/goal split is
  finalized. This file is committed *with the code* so the reasoning travels with it.

---

## 12. Model panel (selected) & launch-day wiring

The candidate selection is **done**; the sheet was scaffolding for it and is not
needed by the program. What the program needs is per-model operational config.
Selection rule was objective: **open-weight + current-generation + chat-capable**.
Capability tiers are deliberately **not** pre-labeled — the capability axis is meant
to emerge from the collected data, not be asserted up front. Architecture is a
**recorded tag**, not the selection spine (the current open-weight frontier is
almost entirely MoE, so a clean Dense-vs-MoE split isn't available). Two
**within-company, within-generation matched pairs** preserve a partial architecture
contrast: Gemma-4-31B (Dense) vs Gemma-4-26B-A4B (MoE), and Qwen3.6-27B (Dense) vs
Qwen3.6-35B-A3B (MoE).

### The 12 (build-now prior)
All are near-certainly **OpenAI-compatible** (`protocol = openai`). This is the
documented prior to build against; the *instance* values are VERIFY-on-launch (below).

| # | Model | Company | Arch | Protocol (prior) | Serving (VERIFY) |
|---|-------|---------|------|------------------|------------------|
| 1 | DeepSeek-V4-Pro | DeepSeek | MoE | openai | native or Fireworks |
| 2 | DeepSeek-V4-Flash | DeepSeek | MoE | openai | native or Fireworks |
| 3 | Kimi-K2.6 | Moonshot | MoE | openai | native or Fireworks |
| 4 | Kimi-K2.5 | Moonshot | MoE | openai | native or Fireworks |
| 5 | MiniMax-M2.5 | MiniMax | MoE | openai | Fireworks (FW-) |
| 6 | GLM-5.2 | Zhipu | MoE | openai | Fireworks (FW-) |
| 7 | Nemotron-3-Super-120B-A12B | NVIDIA | MoE | openai | Fireworks (FW-) |
| 8 | Qwen3.6-35B-A3B | Alibaba | MoE | openai | Fireworks (FW-) |
| 9 | Qwen3.6-27B | Alibaba | Dense* | openai | Fireworks (FW-) |
| 10 | Gemma-4-26B-A4B | Google | MoE | openai | Fireworks (FW-) |
| 11 | Gemma-4-31B | Google | Dense | openai | Fireworks (FW-) |
| 12 | Qwen3.5-397B-A17B | Alibaba | MoE | openai | Fireworks (FW-) |

\* Qwen3.6-27B Dense is a moderate-confidence architecture call — verify before
relying on it as a matched-pair anchor.

**Serving path matters even though protocol is the same:** Fireworks-served (`FW-`)
deployments are OpenAI-compatible but sit at a *different base URL / path* (and
possibly different auth) than native Foundry deployments. That's exactly why
`BaseURL` is a per-model `ModelConfig` field (§6), not a global constant. Same
`protocol` tag, different `BaseURL`.

### VERIFY-on-launch (these values only exist once the account/deployments exist)
Per model, fill into the ModelConfig slot that is **already built** pre-launch:
- `BaseURL` — exact endpoint (native Foundry vs Fireworks path differ)
- `Deployment` — the deployment string, if the native Azure-OpenAI path is used
- `TPM` / `RPM` — your assigned per-deployment quotas (drive the §8 throttle)
- `AuthScope` / credential — confirm the Cognitive Services token scope live
- The OpenAI-compatible vs native Azure-OpenAI request shape (§7) per deployment

**Launch day is verification, not construction.** If the build is truly done, day
one is: provision via Bicep → read back the values above → drop them into config →
confirm managed-identity auth connects live → shakedown run. Every VERIFY item must
have its ModelConfig **slot already built** beforehand, so launch day is *fill-in +
confirm*, never *write code*. Clear the whole day — not for building under credit
pressure, but because first-contact auth/endpoint/quota reality *will* surprise you
and you want slack to debug without burning the window.

### Closed frontier models (deferred, additive, week-1 gated)
Closed models (e.g. `gpt-5.x`, `claude-*`) are **not** in the panel now and may be
added after **week-1 measured cost** shows headroom against the $200 window. Rules:
- **Additive, never destructive.** Adding closed rows extends the dataset; the
  architecture analysis simply runs on the known-architecture subset and the closed
  rows are absent from *that one* graphic. Nothing is removed; no other cut breaks.
- They are a **cost/capability ceiling reference**, not part of the architecture
  contrast (architecture = `Closed` for them).
- **Gate on real data:** extrapolate week-1 cost-per-job to the remaining weeks;
  one frontier closed model can cost several open ones combined. "A couple OpenAI +
  an Anthropic" may be one slot's worth of budget — the data decides.
- Mid-run additions get **fewer days**, so they yield a partial (cost/capability)
  comparison, not the many-repeat consistency data. Add on the *same panel &
  conditions* or they aren't comparable.
- **Anthropic is the one code-touching add:** `claude-*` on Foundry uses the
  **Messages** API, not OpenAI-compatible — it needs the third protocol scorer
  (`geminiScorer`/`openaiScorer` → three-way). Budget ~30 min, not a config row.
