package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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
	PayInputRanges []greenhousePayRange `json:"pay_input_ranges"`
}

type greenhousePayRange struct {
	Title        string  `json:"title"`
	MinCents     float64 `json:"min_cents"`
	MaxCents     float64 `json:"max_cents"`
	CurrencyType string  `json:"currency_type"`
	Interval     string  `json:"interval"`
}

func fetchGreenhouse(ctx context.Context, a *App, company string) ([]Job, error) {
	url := fmt.Sprintf("https://boards-api.greenhouse.io/v1/boards/%s/jobs?content=true&pay_input_ranges=true", company)

	result, err := fetchJSON[greenhouseResponse](ctx, a.Client, url)
	if err != nil {
		return nil, err
	}

	a.Logger.Info("fetched jobs from greenhouse",
		slog.String("company", company),
		slog.Int("count", len(result.Jobs)))
	return greenhouseToJobs(a, result, company)
}

func greenhouseToJobs(a *App, response greenhouseResponse, company string) ([]Job, error) {
	jobs := make([]Job, 0, len(response.Jobs))

	for _, ghJob := range response.Jobs {
		updatedAt, err := time.Parse(time.RFC3339, ghJob.UpdatedAt)
		if err != nil {
			a.Logger.Warn("could not parse posting date",
				slog.String("source", "greenhouse:"+company),
				slog.String("title", ghJob.Title),
				slog.String("raw", ghJob.UpdatedAt))
		}
		timestamp := updatedAt.Unix()
		job := Job{
			Key:         fmt.Sprintf("%s%s%d", strings.Join(strings.Fields(company), ""), strings.Join(strings.Fields(ghJob.Title), ""), timestamp),
			Title:       ghJob.Title,
			Company:     company,
			Location:    ghJob.Location.Name,
			Description: ghJob.Content,
			URL:         ghJob.AbsoluteURL,
			Source:      "greenhouse:" + company,
			PostedAt:    timestamp,
			IsRemote:    greenhouseIsRemote(ghJob.Location.Name, ghJob.Content),
			Salary:      greenhouseSalary(ghJob.PayInputRanges),
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func greenhouseSalary(ranges []greenhousePayRange) *Salary {
	for _, r := range ranges {
		if r.MinCents > 0 || r.MaxCents > 0 {
			return &Salary{
				Min:      float64(r.MinCents) / 100,
				Max:      float64(r.MaxCents) / 100,
				Currency: r.CurrencyType,
				Period:   r.Interval,
			}
		}
	}
	return &Salary{}
}

func greenhouseIsRemote(location, description string) bool {
	return strings.Contains(strings.ToLower(location), "remote") || strings.Contains(strings.ToLower(description), "remote")
}
