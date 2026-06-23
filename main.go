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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/joho/godotenv"
	"google.golang.org/genai"
)

type App struct {
	Logger          *slog.Logger
	Client          *http.Client
	s3Client        *s3.Client
	s3Result        string
	s3Logs          string
	dynamoClient    *dynamodb.Client
	geminikey       string
	aiModel         *genai.Client
	dynamoTableName string
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("error loading .env file: %v", err)
	}
	logsBucket := os.Getenv("S3LOGS")
	logger, logBuf, err := initLogger()
	if err != nil {
		fmt.Printf("error initializing logger: %v", err)
		os.Exit(1)
	}
	resultsBucket := os.Getenv("S3RESULTS")
	if resultsBucket == "" {
		log.Fatal("S3RESULTS env var not set")
	}
	dynamoTableName := os.Getenv("DYNAMOTABLE")
	if dynamoTableName == "" {
		log.Fatal("DYNAMOTABLE env var not set")
	}
	s3Region := os.Getenv("S3REGION")
	if s3Region == "" {
		log.Fatal("S3REGION env var not set")
	}
	geminikey := os.Getenv("GEMINIKEY")
	if geminikey == "" {
		log.Fatal("GEMINIKEY env var not set")
	}
	ctx := context.Background()

	model, err := genai.NewClient(ctx, nil)
	if err != nil {
		log.Fatal("error creating AI client")
	}

	Config, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Region))
	if err != nil {
		log.Fatalf("error configuring s3 file: %v", err)
	}
	dynamoClient := dynamodb.NewFromConfig(Config)
	s3client := s3.NewFromConfig(Config)
	a := App{
		Logger:          logger,
		Client:          httpClient,
		s3Client:        s3client,
		s3Result:        resultsBucket,
		s3Logs:          logsBucket,
		dynamoClient:    dynamoClient,
		geminikey:       geminikey,
		aiModel:         model,
		dynamoTableName: dynamoTableName,
	}

	all := collect(ctx, &a)
	filter, err := LoadKeywordFilter("filterKeywords.json")
	if err != nil {
		a.Logger.Error("cannot load filtering data ", "err", err)
		log.Fatalf("error loading filter file: %v", err)
	}
	matched := filterJobs(all, filter)
	a.Logger.Info("matched jobs", "count", len(matched))

	const batchSize = 20
	var ranked []RankedJob
	for i := 0; i < len(matched); i += batchSize {
		end := min(i+batchSize, len(matched))
		batch, err := getScores(ctx, &a, matched[i:end])
		if err != nil {
			slog.Error("scoring batch failed", "start", i, "err", err)
			continue
		}
		ranked = append(ranked, batch...)
	}
	if err := writeResultsToS3(ctx, &a, matched); err != nil {
		a.Logger.Error("error writing to S3", "error", err)
	}
	if err := writeLogs(ctx, &a, logBuf.Bytes()); err != nil {
		log.Fatalf("error writing logs: %v", err)
	}
	writeResultsToDynamoDB(ctx, &a, ranked)
}
