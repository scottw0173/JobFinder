package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type Job struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Company     string  `json:"company"`
	Location    string  `json:"location"`
	Description string  `json:"description"`
	URL         string  `json:"url"`
	Source      string  `json:"source"` // provenance, e.g. "greenhouse:stripe"
	PostedAt    int64   `json:"posted_at"`
	IsRemote    bool    `json:"is_remote,omitempty"`
	Salary      *Salary `json:"salary,omitempty"`
}

type Salary struct {
	Min      float64 `json:"min_salary"`
	Max      float64 `json:"max_salary"`
	Currency string  `json:"currency"`
	Period   string  `json:"period"` // yearly or monthly
}

func createSourcesMap(ctx context.Context, a *App) (map[string][]string, error) {

	data, err := getS3Object(ctx, a, "sources.json")
	if err != nil {
		a.Logger.Error("cannot read sources.json", "err", err)
		return nil, err
	}
	var sources map[string][]string
	if err := json.Unmarshal(data, &sources); err != nil {
		a.Logger.Error("cannot unmarshal sources.json", "err", err)
		return nil, err
	}
	return sources, nil
}

func fetchJSON[T any](ctx context.Context, app *App, url string) (T, error) {
	var result T

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		app.Logger.Error("failed to create request", slog.String("error", err.Error()))
		return result, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := app.Client.Do(req)
	if err != nil {
		app.Logger.Error("failed to do request", slog.String("error", err.Error()))
		return result, fmt.Errorf("failed to do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		app.Logger.Error("unexpected status code", slog.Int("status", resp.StatusCode))
		return result, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		app.Logger.Error("failed to decode response", slog.String("error", err.Error()))
		return result, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

func filterJobs(jobs []Job, filter *KeywordFilter) []Job {
	var out []Job
	for _, j := range jobs {
		if j.IsRemote && filter.Matches(j.Title) {
			out = append(out, j)
		}
	}
	return out
}

func collect(ctx context.Context, a *App) ([]Job, error) {
	sources, err := createSourcesMap(ctx, a)
	if err != nil {
		return nil, err
	}

	fetchers := map[string]func(context.Context, *App, string) ([]Job, error){
		"greenhouse": fetchGreenhouse,
		"ashby":      fetchAshby,
		"lever":      fetchLever,
	}

	var all []Job
	for provider, companies := range sources {
		fn, ok := fetchers[provider]
		if !ok {
			a.Logger.Warn("no fetcher for provider", "provider", provider)
			continue
		}
		for _, c := range companies {
			jobs, err := fn(ctx, a, c)
			if err != nil {
				a.Logger.Warn("fetch failed", "provider", provider, "company", c, "err", err)
				continue
			}
			all = append(all, jobs...)
		}
	}
	a.Logger.Info("collected jobs", "count", len(all))
	return all, nil
}

func fetchSecret(ctx context.Context, cfg aws.Config, paramName string) (string, error) {
	client := ssm.NewFromConfig(cfg)
	out, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &paramName,
		WithDecryption: aws.Bool(true), // SecureString -> decrypt
	})
	if err != nil {
		return "", fmt.Errorf("ssm get %s: %w", paramName, err)
	}
	return *out.Parameter.Value, nil
}

func getS3Object(ctx context.Context, a *App, key string) ([]byte, error) {
	output, err := a.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.s3Config),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, err
	}

	return data, nil
}

/*func writeResultsToS3(ctx context.Context, a *App, jobs []Job) error {
	data, err := json.Marshal(jobs)
	if err != nil {
		return err
	}
	identifier := fmt.Sprintf("jobs-%s.json", time.Now().Format("2006-01-02T15-04-05"))
	_, err = a.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.s3Result),
		Key:         aws.String(identifier),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to write to S3: %w", err)
	}
	return nil
}*/
