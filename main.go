package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type App struct {
	Logger *slog.Logger
	Client *http.Client
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func main() {
	logger, closer, err := initLogger("")
	if err != nil {
		fmt.Printf("error initializing logger: %v", err)
		os.Exit(1)
	}
	defer closer()
	a := App{
		Logger: logger,
		Client: httpClient,
	}
	var postingsJobs []Job
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, company, err := fetchGreenhouse(ctx, &a, "dragos")
	if err != nil {
		a.Logger.Error(fmt.Sprintf("error fetching greenhouse for %s", company))
	}
	a.Logger.Info(fmt.Sprintf("fetched %d jobs from Greenhouse for %s", len(results.Jobs), company))
	jobs, err := greenhouseToJobs(results, company)
	postingsJobs = append(postingsJobs, jobs...)
	for _, job := range postingsJobs {
		fmt.Println(job.Title)
	}
}
