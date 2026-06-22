package main

import (
	"time"
)

// Job is a single posting, normalized from whatever source it came from.
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
	Min      int    `json:"min_salary"`
	Max      int    `json:"max_salary"`
	Currency string `json:"currency"`
	Period   string `json:"period"` // yearly or monthly
}

func greenhouseToJobs(response greenhouseResponse, company string) ([]Job, error) {
	jobs := make([]Job, 0, len(response.Jobs))

	for _, ghJob := range response.Jobs {
		updatedAt, _ := time.Parse(time.RFC3339, ghJob.UpdatedAt)
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
