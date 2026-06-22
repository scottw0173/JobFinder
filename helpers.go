package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type Job struct {
	Title       string    `json:"title"`
	Company     string    `json:"company"`
	Location    string    `json:"location"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	Source      string    `json:"source"` // provenance, e.g. "greenhouse:stripe"
	PostedAt    time.Time `json:"posted_at"`
	IsRemote    bool      `json:"is_remote,omitempty"`
	Salary      *Salary   `json:"salary,omitempty"`
}

type Salary struct {
	Min      float64 `json:"min_salary"`
	Max      float64 `json:"max_salary"`
	Currency string  `json:"currency"`
	Period   string  `json:"period"` // yearly or monthly
}

func createSourcesMap(a *App) map[string][]string {

	data, err := os.ReadFile("sources.json")
	if err != nil {
		a.Logger.Error("cannot read sources.json", "err", err)
		os.Exit(1)
	}
	var sources map[string][]string
	if err := json.Unmarshal(data, &sources); err != nil {
		a.Logger.Error("cannot unmarshal sources.json", "err", err)
		os.Exit(1)
	}
	return sources
}

func fetchJSON[T any](ctx context.Context, client *http.Client, url string) (T, error) {
	var result T

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return result, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return result, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}
