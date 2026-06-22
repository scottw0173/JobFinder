package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type greenhouseResponse struct {
	Jobs []greenhouseJob `json:"jobs"`
}

type greenhouseJob struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	AbsoluteURL string `json:"absolute_url"`
	UpdatedAt   string `json:"updated_at"`
	Location    struct {
		Name string `json:"name"`
	} `json:"location"`
}

func fetchGreenhouse(ctx context.Context, app *App, company string) (greenhouseResponse, string, error) {
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true", company)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return greenhouseResponse{}, "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := app.Client.Do(req)
	if err != nil {
		return greenhouseResponse{}, "", fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return greenhouseResponse{}, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result greenhouseResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return greenhouseResponse{}, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result, company, nil
}
