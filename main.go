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

	all := collect(ctx, &a)
	filter, err := LoadKeywordFilter("filterKeywords.json")
	if err != nil {
		a.Logger.Error("cannot load filtering data ", "err", err)
		log.Fatalf("error loading filter file: %v", err)
	}
	matched := filterJobs(all, filter)
	a.Logger.Info("matched jobs", "count", len(matched))
}
