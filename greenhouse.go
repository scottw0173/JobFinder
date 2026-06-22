package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
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

func fetchGreenhouse(ctx context.Context, app *App, company string) ([]Job, error) {
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true", company)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return []Job{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := app.Client.Do(req)
	if err != nil {
		return []Job{}, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []Job{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result greenhouseResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return []Job{}, fmt.Errorf("failed to decode response: %w", err)
	}
	app.Logger.Info("fetched jobs from greenhouse",
		slog.String("company", company),
		slog.Int("count", len(result.Jobs)))
	return greenhouseToJobs(app, result, company)
}

func greenhouseToJobs(app *App, response greenhouseResponse, company string) ([]Job, error) {
	jobs := make([]Job, 0, len(response.Jobs))

	for _, ghJob := range response.Jobs {
		updatedAt, err := time.Parse(time.RFC3339, ghJob.UpdatedAt)
		if err != nil {
			app.Logger.Warn("could not parse posting date",
				slog.String("source", "greenhouse:"+company),
				slog.String("title", ghJob.Title),
				slog.String("raw", ghJob.UpdatedAt))
		}
		job := Job{
			Title:       ghJob.Title,
			Company:     company,
			Location:    ghJob.Location.Name,
			Description: ghJob.Content,
			URL:         ghJob.AbsoluteURL,
			Source:      "greenhouse:" + company,
			PostedAt:    updatedAt,
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}
