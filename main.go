package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
)

type App struct {
	Logger   *slog.Logger
	Client   *http.Client
	s3Client *s3.Client
	s3Result string
	s3Logs   string
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("error loading .env file: %v", err)
	}
	logsBucket := os.Getenv("S3LOGS")
	if logsBucket == "" {
		log.Fatal("S3LOGS env var not set")
	}
	logFile := logsBucket
	logger, closer, err := initLogger(logFile)
	if err != nil {
		fmt.Printf("error initializing logger: %v", err)
		os.Exit(1)
	}
	defer closer()
	resultsBucket := os.Getenv("S3RESULTS")
	if resultsBucket == "" {
		log.Fatal("S3RESULTS env var not set")
	}
	s3Region := os.Getenv("S3REGION")
	if s3Region == "" {
		log.Fatal("S3REGION env var not set")
	}
	ctx := context.Background()
	s3Config, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Region))
	if err != nil {
		log.Fatalf("error configuring s3 file: %v", err)
	}

	client := s3.NewFromConfig(s3Config)
	a := App{
		Logger:   logger,
		Client:   httpClient,
		s3Client: client,
		s3Result: resultsBucket,
		s3Logs:   logsBucket,
	}

	all := collect(ctx, &a)
	filter, err := LoadKeywordFilter("filterKeywords.json")
	if err != nil {
		a.Logger.Error("cannot load filtering data ", "err", err)
		log.Fatalf("error loading filter file: %v", err)
	}
	matched := filterJobs(all, filter)
	a.Logger.Info("matched jobs", "count", len(matched))
	if err := writeResults(&a, matched); err != nil {
		log.Fatalf("error writing results: %v", err)
	}
}
