package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type App struct {
	Logger *slog.Logger
	Client *http.Client
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("error loading .env file: %v", err)
	}
	logFile := os.Getenv("LOGFILE")
	logger, closer, err := initLogger(logFile)
	if err != nil {
		fmt.Printf("error initializing logger: %v", err)
		os.Exit(1)
	}
	defer closer()
	a := App{
		Logger: logger,
		Client: httpClient,
	}
	ctx := context.Background()

	sources := createSourcesMap(&a)

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
			jobs, err := fn(ctx, &a, c)
			if err != nil {
				a.Logger.Warn("fetch failed", "provider", provider, "company", c, "err", err)
				continue
			}
			all = append(all, jobs...)
		}
	}
	filter, err := LoadKeywordFilter("filterKeywords.json")
	if err != nil {
		a.Logger.Error("cannot load filtering data ")
		log.Fatalf("error loading filter file: %v", err)
	}
	var matches []Job
	for _, listing := range all {
		if filter.Matches(listing.Title) {
			matches = append(matches, listing)
		}
	}
	a.Logger.Info("matched jobs", "count", len(matches))
	var remoteMatches []Job
	for _, listing := range matches {
		if listing.IsRemote {
			remoteMatches = append(remoteMatches, listing)
		}
	}
	a.Logger.Info("matched jobs", "count", len(remoteMatches))
	for _, listing := range remoteMatches {
		fmt.Printf("Company: %s\t\tTitle: %s\n", listing.Company, listing.Title)
	}
}
