package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"google.golang.org/genai"
)

type App struct {
	Logger             *slog.Logger
	LogBuffer          *bytes.Buffer
	Client             *http.Client
	s3Client           *s3.Client
	s3Config           string
	s3Logs             string
	dynamoClient       *dynamodb.Client
	geminiInstructions []byte
	geminikey          string
	aiModel            *genai.Client
	dynamoTableName    string
}

var app *App

func main() {
	logsBucket := os.Getenv("S3LOGS")
	logger, logBuf, err := initLogger()
	if err != nil {
		fmt.Printf("error initializing logger: %v", err)
		os.Exit(1)
	}
	dynamoTableName := os.Getenv("DYNAMOTABLE")
	if dynamoTableName == "" {
		log.Fatal("DYNAMOTABLE env var not set")
	}
	s3Region := os.Getenv("S3REGION")
	if s3Region == "" {
		log.Fatal("S3REGION env var not set")
	}
	ctx := context.Background()

	config, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Region))
	if err != nil {
		log.Fatalf("error configuring s3 file: %v", err)
	}
	geminikey, err := fetchSecret(ctx, config, os.Getenv("GEMINIAPIKEY"))
	if err != nil {
		log.Fatalf("error fetching gemini key: %v", err)
	}
	model, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminikey})
	if err != nil {
		log.Fatal("error creating AI client")
	}
	dynamoClient := dynamodb.NewFromConfig(config)
	s3client := s3.NewFromConfig(config)
	app = &App{
		Logger:          logger,
		LogBuffer:       logBuf,
		Client:          &http.Client{Timeout: 60 * time.Second},
		s3Client:        s3client,
		s3Config:        os.Getenv("S3CONFIG"),
		s3Logs:          logsBucket,
		dynamoClient:    dynamoClient,
		geminikey:       geminikey,
		aiModel:         model,
		dynamoTableName: dynamoTableName,
	}
	instructions, err := getS3Object(ctx, app, "instructions.md")
	if err != nil {
		log.Fatalf("error getting instructions: %v", err)
	}
	app.geminiInstructions = instructions
	lambda.Start(handler)
}

func handler(ctx context.Context, _ json.RawMessage) error {
	all, err := collect(ctx, app)
	if err != nil {
		app.Logger.Error("cannot collect jobs", "err", err)
		return fmt.Errorf("error collecting jobs: %w", err)
	}
	filter, err := LoadKeywordFilter(ctx, app)
	if err != nil {
		app.Logger.Error("cannot load filtering data ", "err", err)
		return fmt.Errorf("error loading filter file: %w", err)
	}
	matched := filterJobs(all, filter)
	app.Logger.Info("matched jobs", "count", len(matched))

	limiter := time.NewTicker(6 * time.Second) // ~10 req/min
	defer limiter.Stop()
	const batchSize = 15
	var ranked []RankedJob
	for i := 0; i < len(matched); i += batchSize {
		<-limiter.C
		end := min(i+batchSize, len(matched))
		batch, err := getScores(ctx, app, matched[i:end])
		if err != nil {
			app.Logger.Error("scoring batch failed", "start", i, "err", err)
			continue
		}
		ranked = append(ranked, batch...)
	}
	if err := writeLogs(ctx, app); err != nil {
		return fmt.Errorf("error writing logs: %w", err)
	}
	return writeResultsToDynamoDB(ctx, app, ranked)
}
