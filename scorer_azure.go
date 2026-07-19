package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// azureScorer talks to any OpenAI-compatible /chat/completions endpoint -
// local Ollama today, real Azure OpenAI/Foundry once the subscription is
// live (only baseURL/apiKey change; this HTTP call shape is designed to
// need no further edits when the real key arrives in step 7). Batches every
// job passed to ScoreBatch into one HTTP call, mirroring geminiScorer.
type azureScorer struct {
	logger       *slog.Logger
	client       *http.Client
	baseURL      string // e.g. http://ollama:11434/v1 - no trailing /chat/completions
	apiKey       string // empty for local Ollama (no auth); real key lands in step 7
	instructions []byte
}

func newAzureScorer(logger *slog.Logger, client *http.Client, baseURL, apiKey string, instructions []byte) *azureScorer {
	return &azureScorer{logger: logger, client: client, baseURL: baseURL, apiKey: apiKey, instructions: instructions}
}

// scoreFormatOverride re-expresses instructions.md's inherited "0 to 10
// integer" OUTPUT convention (shared prose, not code) on the 0-100 float
// scale CLAUDE.md wants for the Azure measurement instrument. Layered onto
// the request rather than editing instructions.md, which is per-candidate
// user content, not code.
const scoreFormatOverride = `

FORMAT OVERRIDE (applies over any scale mentioned above): express the final
score as a number from 0 to 100, not 0 to 10. Convert proportionally (a
7-out-of-10 judgment becomes 70) - do not invent precision the rubric above
doesn't support; one decimal place is the most you should ever need.`

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type jsonSchemaFormat struct {
	Type       string         `json:"type"` // "json_schema"
	JSONSchema jsonSchemaSpec `json:"json_schema"`
}

type jsonSchemaSpec struct {
	Name   string `json:"name"`
	Strict bool   `json:"strict"`
	Schema any    `json:"schema"`
}

type chatCompletionRequest struct {
	Model          string           `json:"model"`
	Messages       []chatMessage    `json:"messages"`
	Temperature    float32          `json:"temperature"`
	ResponseFormat jsonSchemaFormat `json:"response_format"`
	Logprobs       bool             `json:"logprobs,omitempty"`
	TopLogprobs    int              `json:"top_logprobs,omitempty"`
}

// azureScoreItem is one entry of the model's "results" array. Score is a
// float - CLAUDE.md's hard rule for Azure (gemini.go's int Score stays as-is
// for AWS; this is a deliberate divergence, not a bug to copy).
type azureScoreItem struct {
	Key       string  `json:"key"`
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning"`
}

// chatCompletionResponse is the OpenAI-compatible envelope both Ollama and
// (later) an OpenAI-compatible Azure OpenAI surface return.
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Logprobs json.RawMessage `json:"logprobs"` // captured verbatim; shape inspected only inside bestEffortScoreEV
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (s *azureScorer) ScoreBatch(ctx context.Context, jobs []Job, model ModelConfig) ([]ScoreResult, float64, error) {
	jobsJSON, err := json.Marshal(toPayload(jobs)) // gemini.go's jobPayload/toPayload - reused, not redefined
	if err != nil {
		wrapped := wrapErr("error marshalling jobs", err)
		s.logger.Error("error marshalling jobs for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"results": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"key":       map[string]any{"type": "string"},
						"score":     map[string]any{"type": "number"},
						"reasoning": map[string]any{"type": "string"},
					},
					"required":             []string{"key", "score", "reasoning"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"results"},
		"additionalProperties": false,
	}
	// Top level is an object wrapping a "results" array (not a bare array) -
	// matches OpenAI's strict-mode constraint that the schema root must be
	// an object; keeps the door open for step 7 unchanged.

	reqBody := chatCompletionRequest{
		Model: model.Name,
		Messages: []chatMessage{
			{Role: "system", Content: string(s.instructions) + scoreFormatOverride},
			{Role: "user", Content: "Jobs to score:\n" + string(jobsJSON)},
		},
		Temperature: model.Temperature, // finally used, unlike gemini.go - set explicitly via AZURE_MODELS locally
		ResponseFormat: jsonSchemaFormat{
			Type:       "json_schema",
			JSONSchema: jsonSchemaSpec{Name: "job_scores", Strict: true, Schema: schema},
		},
	}
	if model.WantLogprobs {
		reqBody.Logprobs = true
		reqBody.TopLogprobs = 5
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		wrapped := wrapErr("error marshalling request", err)
		s.logger.Error("error marshalling request for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}

	url := strings.TrimSuffix(s.baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		wrapped := wrapErr("error creating request", err)
		s.logger.Error("error creating request for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		// OpenAI-compatible convention. Real Azure OpenAI's native surface
		// may want "api-key" and a /openai/deployments/{deployment}/...
		// path instead - reconciling that (and finally using
		// ModelConfig.Deployment) is step 7's job, not guessed at here.
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		wrapped := wrapErr("error doing request", err)
		s.logger.Error("error doing request for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		s.logger.Error("non-200 from openai-compatible endpoint",
			slog.Int("status", resp.StatusCode), slog.String("body", string(body)))
		return nil, 0, &statusError{code: resp.StatusCode} // classify() handles 5xx retry, unchanged
	}

	var cr chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		wrapped := wrapErr("failed to decode envelope", err)
		s.logger.Error("error decoding response for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}
	if len(cr.Choices) == 0 {
		traced := traceErrorf("empty response from openai-compatible endpoint")
		s.logger.Error("empty response for scoring", errAttr(traced))
		return nil, 0, traced
	}

	var envelope struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &envelope); err != nil {
		wrapped := wrapErr("failed to decode scores", err)
		s.logger.Error("failed to decode scores for scoring", errAttr(wrapped))
		return nil, 0, wrapped
	}

	logprobs := cr.Choices[0].Logprobs
	now := time.Now()
	out := make([]ScoreResult, 0, len(envelope.Results))
	for _, raw := range envelope.Results {
		var item azureScoreItem
		if err := json.Unmarshal(raw, &item); err != nil {
			// Per-item, not batch-fatal: zipScoreEvents already logs+skips
			// any job with no matching JobKey, so dropping here just routes
			// into that existing path.
			s.logger.Warn("dropping malformed score item", errAttr(wrapErr("unmarshal score item", err)))
			continue
		}

		score := item.Score
		var lp json.RawMessage
		if model.WantLogprobs && len(logprobs) > 0 {
			// Same batch-level logprobs blob is stored verbatim on every
			// result in this batch (see bestEffortScoreEV doc comment) -
			// "store raw" is satisfied even though it's duplicated per row.
			lp = logprobs
			if ev, ok := bestEffortScoreEV(cr.Choices[0].Message.Content, logprobs, item.Key, item.Score); ok {
				score = ev
			}
		}

		out = append(out, ScoreResult{
			JobKey:    item.Key,
			Model:     model.Name,
			Score:     score,
			Reasoning: item.Reasoning,
			Raw:       raw, // verbatim per-item bytes as emitted - never empty for anything appended here
			Logprobs:  lp,
			ScoredAt:  now,
		})
	}
	return out, float64(cr.Usage.TotalTokens), nil
}

// bestEffortScoreEV attempts a probability-weighted expected value over the
// score token(s) for one job's result inside a BATCHED completion (ScoreBatch
// scores up to 5 jobs per HTTP call, so content/logprobsRaw cover every
// job's output together, not just this one).
//
// First-pass heuristic, expected to fall back to the emitted number often in
// practice. Falls back (returns ok=false; caller keeps the emitted number)
// when: logprobsRaw is empty or doesn't parse into the expected token shape;
// the job's key or a "score" field after it can't be found in content; the
// token at that offset isn't a single clean numeric token (a multi-token
// split numeral is a known gap, not attempted here); or any top_logprobs
// candidate at that token isn't itself numeric - treated as unreliable, not
// silently excluded. Refine after testing against real local-model output.
func bestEffortScoreEV(content string, logprobsRaw json.RawMessage, key string, emitted float64) (float64, bool) {
	var lp struct {
		Content []struct {
			Token       string  `json:"token"`
			Logprob     float64 `json:"logprob"`
			TopLogprobs []struct {
				Token   string  `json:"token"`
				Logprob float64 `json:"logprob"`
			} `json:"top_logprobs"`
		} `json:"content"`
	}
	if err := json.Unmarshal(logprobsRaw, &lp); err != nil || len(lp.Content) == 0 {
		return emitted, false
	}

	keyIdx := strings.Index(content, `"`+key+`"`)
	if keyIdx < 0 {
		return emitted, false
	}
	scoreLabel := strings.Index(content[keyIdx:], `"score"`)
	if scoreLabel < 0 {
		return emitted, false
	}
	targetOffset := keyIdx + scoreLabel + len(`"score":`)

	offset, tokenIdx := 0, -1
	for i, t := range lp.Content {
		if offset >= targetOffset {
			tokenIdx = i
			break
		}
		offset += len(t.Token)
	}
	for tokenIdx >= 0 && tokenIdx < len(lp.Content) && strings.TrimSpace(lp.Content[tokenIdx].Token) == "" {
		tokenIdx++
	}
	if tokenIdx < 0 || tokenIdx >= len(lp.Content) {
		return emitted, false
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(lp.Content[tokenIdx].Token), 64); err != nil {
		return emitted, false // not a clean single numeric token
	}

	var weighted, weightSum float64
	for _, cand := range lp.Content[tokenIdx].TopLogprobs {
		v, err := strconv.ParseFloat(strings.TrimSpace(cand.Token), 64)
		if err != nil {
			return emitted, false // a non-numeric candidate makes the distribution unreliable
		}
		w := math.Exp(cand.Logprob)
		weighted += w * v
		weightSum += w
	}
	if weightSum == 0 {
		return emitted, false
	}
	return weighted / weightSum, true
}
