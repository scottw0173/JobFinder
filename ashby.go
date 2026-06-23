package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type ashbyResponse struct {
	Jobs []ashbyJob `json:"jobs"`
}

type ashbyJob struct {
	Title           string            `json:"title"`
	Location        string            `json:"location"`
	Department      string            `json:"department"`
	Team            string            `json:"team"`
	IsListed        bool              `json:"isListed"`
	IsRemote        bool              `json:"isRemote"`
	DescriptionHTML string            `json:"descriptionHtml"`
	PublishedAt     string            `json:"publishedAt"`
	EmploymentType  string            `json:"employmentType"`
	JobURL          string            `json:"jobUrl"`
	Compensation    ashbyCompensation `json:"compensation"`
}

type ashbyCompensation struct {
	Tiers []ashbyTier `json:"compensationTiers"`
}

type ashbyTier struct {
	Components []ashbyComponent `json:"components"`
}

type ashbyComponent struct {
	CompensationType string  `json:"compensationType"`
	CurrencyCode     string  `json:"currencyCode"`
	Interval         string  `json:"interval"`
	MinValue         float64 `json:"minValue"`
	MaxValue         float64 `json:"maxValue"`
}

func fetchAshby(ctx context.Context, app *App, company string) ([]Job, error) {
	url := fmt.Sprintf("https://api.ashbyhq.com/posting-api/job-board/%s?includeCompensation=true", company)

	result, err := fetchJSON[ashbyResponse](ctx, app.Client, url)
	if err != nil {
		return nil, err
	}

	app.Logger.Info("fetched jobs from ashby",
		slog.String("company", company),
		slog.Int("count", len(result.Jobs)))

	return ashbyToJobs(app, result, company), nil
}

func ashbyToJobs(app *App, result ashbyResponse, company string) []Job {
	jobs := make([]Job, 0, len(result.Jobs))

	for _, aj := range result.Jobs {
		if !aj.IsListed {
			continue
		}

		var postedAt time.Time
		if aj.PublishedAt != "" {
			t, err := time.Parse(time.RFC3339, aj.PublishedAt)
			if err != nil {
				app.Logger.Warn("failed to parse ashby publishedAt",
					slog.String("company", company),
					slog.String("value", aj.PublishedAt))
			} else {
				postedAt = t
			}
		}

		timestamp := postedAt.Unix()
		jobs = append(jobs, Job{
			Key:         fmt.Sprintf("%s%s%d", strings.Join(strings.Fields(company), ""), strings.Join(strings.Fields(aj.Title), ""), timestamp),
			Title:       aj.Title,
			Company:     company,
			Location:    aj.Location,
			Description: aj.DescriptionHTML,
			URL:         aj.JobURL,
			Source:      "ashby",
			PostedAt:    timestamp,
			IsRemote:    aj.IsRemote,
			Salary:      ashbySalary(aj.Compensation),
		})
	}

	return jobs
}

func ashbySalary(comp ashbyCompensation) *Salary {
	for _, tier := range comp.Tiers {
		for _, c := range tier.Components {
			if c.CompensationType == "Salary" && (c.MinValue > 0 || c.MaxValue > 0) {
				return &Salary{
					Min:      c.MinValue,
					Max:      c.MaxValue,
					Currency: c.CurrencyCode,
					Period:   c.Interval,
				}
			}
		}
	}
	return &Salary{}
}
