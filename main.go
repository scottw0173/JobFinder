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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sources := createSourcesMap(&a)

	fetchers := map[string]func(context.Context, *App, string) ([]Job, error){
		"greenhouse": fetchGreenhouse,
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
	for _, listing := range all {
		fmt.Println(listing.Title)
	}
}
