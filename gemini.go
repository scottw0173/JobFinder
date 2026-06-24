package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

type RankedJob struct {
	Job
	Stablekey string
	Score     int
	Reasoning string
}

type scoreResult struct {
	Key       string `json:"key"`
	Score     int    `json:"score"`
	Reasoning string `json:"reasoning"`
}

type geminiRequest struct {
	Contents         []content        `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type content struct {
	Parts []part `json:"parts"`
}
type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	ResponseMIMEType string `json:"responseMimeType"`
	ResponseSchema   any    `json:"responseSchema"`
}

type jobPayload struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	Description string `json:"description"`
}

func getScores(ctx context.Context, a *App, jobs []Job) ([]RankedJob, int, error) {
	jobsJSON, err := json.Marshal(toPayload(jobs)) // []jobPayload
	if err != nil {
		a.Logger.Error("error marshalling jobs for scoring", slog.String("error", err.Error()))
		return []RankedJob{}, 0, fmt.Errorf("error marshalling jobs: %w", err)
	}
	var scoreSchema = map[string]any{ //schema for how gemini response comes in
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":       map[string]any{"type": "string"},
				"score":     map[string]any{"type": "integer"},
				"reasoning": map[string]any{"type": "string"},
			},
			"required":         []string{"key", "score", "reasoning"},
			"propertyOrdering": []string{"key", "score", "reasoning"},
		},
	}
	reqBody := geminiRequest{
		Contents: []content{{Parts: []part{
			{Text: string(a.geminiInstructions)},
			{Text: "Jobs to score:\n" + string(jobsJSON)},
		}}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   scoreSchema,
		},
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		a.Logger.Error("error marshalling request for scoring", slog.String("error", err.Error()))
		return []RankedJob{}, 0, fmt.Errorf("error marshalling request: %w", err)
	}
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.1-flash-lite:generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		a.Logger.Error("error creating request for scoring", slog.String("error", err.Error()))
		return []RankedJob{}, 0, fmt.Errorf("error creating request: %w", err)
	}
	req.Header.Set("x-goog-api-key", a.geminikey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Client.Do(req)
	if err != nil {
		a.Logger.Error("error doing request for scoring", slog.String("error", err.Error()))
		return []RankedJob{}, 0, fmt.Errorf("error doing request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		a.Logger.Error("unexpected status code for scoring", slog.Int("status", resp.StatusCode))
		return nil, 0, &statusError{code: resp.StatusCode}
	}

	var gr struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		a.Logger.Error("error decoding response for scoring", slog.String("error", err.Error()))
		return nil, 0, fmt.Errorf("failed to decode envelope: %w", err)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		a.Logger.Error("empty response from gemini for scoring")
		return nil, 0, fmt.Errorf("empty response from gemini")
	}
	var result []scoreResult
	if err := json.Unmarshal([]byte(gr.Candidates[0].Content.Parts[0].Text), &result); err != nil {
		a.Logger.Error("failed to decode scores for scoring", slog.String("error", err.Error()))
		return nil, 0, fmt.Errorf("failed to decode scores: %w", err)
	}
	/*a.Logger.Info("gemini token usage",
	"prompt", gr.UsageMetadata.PromptTokenCount,
	"total", gr.UsageMetadata.TotalTokenCount,
	"batch_size", len(jobs))*/
	return rankJobs(a, jobs, result), gr.UsageMetadata.TotalTokenCount, nil
}

func toPayload(jobs []Job) []jobPayload {
	out := make([]jobPayload, len(jobs))
	for i, j := range jobs {
		out[i] = jobPayload{
			Key:         j.Key,
			Title:       j.Title,
			Company:     j.Company,
			Location:    j.Location,
			Description: j.Description,
		}
	}
	return out
}

func rankJobs(a *App, jobs []Job, results []scoreResult) []RankedJob {
	byKey := make(map[string]scoreResult, len(results))
	for _, r := range results {
		byKey[r.Key] = r
	}

	ranked := make([]RankedJob, 0, len(jobs))
	for _, j := range jobs {
		r, ok := byKey[j.Key]
		if !ok {
			a.Logger.Warn("no score returned for job", "key", j.Key)
			continue
		}
		ranked = append(ranked, newRankedJob(j, r))
	}
	return ranked
}

func newRankedJob(j Job, r scoreResult) RankedJob {
	return RankedJob{
		Job:       j,
		Stablekey: strings.Join(strings.Fields(j.Company+j.Title+j.Location), ""),
		Score:     r.Score,
		Reasoning: r.Reasoning,
	}
}
