package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

/*type leverResponse struct {
	Jobs []leverPosting `json:"jobs"`
}*/

type leverPosting struct {
	ID               string `json:"id"`
	Text             string `json:"text"` // job title
	HostedURL        string `json:"hostedUrl"`
	ApplyURL         string `json:"applyUrl"`
	WorkplaceType    string `json:"workplaceType"`
	CreatedAt        int64  `json:"createdAt"`
	DescriptionPlain string `json:"descriptionPlain"`

	Categories struct {
		Commitment string `json:"commitment"`
		Department string `json:"department"`
		Location   string `json:"location"`
		Team       string `json:"team"`
	} `json:"categories"`

	SalaryRange *struct {
		Min      float64 `json:"min"`
		Max      float64 `json:"max"`
		Currency string  `json:"currency"`
		Interval string  `json:"interval"` // e.g. "per-year-salary"
	} `json:"salaryRange"`
}

func fetchLever(ctx context.Context, app *App, company string) ([]Job, error) {
	url := fmt.Sprintf("https://api.lever.co/v0/postings/%s?mode=json", company)

	postings, err := fetchJSON[[]leverPosting](ctx, app.Client, url)
	if err != nil {
		return nil, err
	}

	app.Logger.Info("fetched jobs from lever",
		slog.String("company", company),
		slog.Int("count", len(postings)))
	return leverToJobs(postings, company)
}

func leverToJobs(resp []leverPosting, company string) ([]Job, error) {
	jobs := make([]Job, 0, len(resp))
	for _, p := range resp {
		postedAt := time.UnixMilli(p.CreatedAt)
		timestamp := postedAt.Unix()
		job := Job{
			Key:         fmt.Sprintf("%s%s%d", strings.Join(strings.Fields(company), ""), strings.Join(strings.Fields(p.Text), ""), timestamp),
			Title:       p.Text,
			Company:     company,
			Location:    p.Categories.Location,
			Description: p.DescriptionPlain,
			URL:         p.HostedURL,
			Source:      "lever:" + company,
			PostedAt:    postedAt,
			IsRemote:    p.WorkplaceType == "remote",
			Salary:      leverSalary(p.SalaryRange),
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func leverSalary(s *struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Currency string  `json:"currency"`
	Interval string  `json:"interval"`
}) *Salary {
	if s == nil {
		return nil
	}
	return &Salary{
		Min:      s.Min,
		Max:      s.Max,
		Currency: s.Currency,
		Period:   leverPeriod(s.Interval),
	}
}

func leverPeriod(interval string) string {
	switch interval {
	case "per-year-salary":
		return "year"
	case "per-month-salary":
		return "month"
	case "per-hour-wage":
		return "hour"
	default:
		return interval // keep raw value rather than guessing
	}
}
