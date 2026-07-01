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
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type App struct {
	Logger             *slog.Logger
	LogBuffer          *bytes.Buffer
	Client             *http.Client
	s3Client           *s3.Client
	s3Config           string
	s3Logs             string
	ssmClient          *ssm.Client
	dynamoClient       *dynamodb.Client
	geminimodel        string
	geminiInstructions []byte
	spreadsheetID      string
	geminikey          string
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
	geminimodel := os.Getenv("GEMINIMODEL")
	if geminimodel == "" {
		log.Fatal("GEMINIMODEL env var not set")
	}
	s3config := os.Getenv("S3CONFIG")
	if s3config == "" {
		log.Fatal("S3CONFIG env var not set")
	}
	spreadsheetID := os.Getenv("SPREADSHEETID")

	ctx := context.Background()

	config, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Region))
	if err != nil {
		log.Fatalf("error configuring s3 file: %v", err)
	}

	dynamoClient := dynamodb.NewFromConfig(config)
	s3client := s3.NewFromConfig(config)
	ssmClient := ssm.NewFromConfig(config)

	app = &App{
		Logger:          logger,
		LogBuffer:       logBuf,
		Client:          &http.Client{Timeout: 60 * time.Second},
		s3Client:        s3client,
		s3Config:        s3config,
		s3Logs:          logsBucket,
		ssmClient:       ssmClient,
		spreadsheetID:   spreadsheetID,
		dynamoClient:    dynamoClient,
		dynamoTableName: dynamoTableName,
	}
	geminikey, err := fetchSecret(ctx, app, os.Getenv("GEMINIAPIKEY"))
	if err != nil {
		log.Fatalf("error fetching gemini key: %v", err)
	}
	app.geminikey = geminikey

	app.Logger.Info("app and logger initialized", "time", time.Now())
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

	seenJobs, err := scanAllJobs(ctx, app)
	if err != nil {
		app.Logger.Error("cannot read results from dynamodb", "err", err)
	}
	//scanOK := err == nil
	seenSet := compositeKeySet(seenJobs)
	var fresh []Job
	app.Logger.Debug("checking again seen set begins", "time", time.Now())
	for _, job := range all {
		if !seenSet[job.createCompositeKey()] {
			fresh = append(fresh, job)
		}
	}
	liveKeys := make(map[string]struct{}, len(all))
	for _, job := range all {
		liveKeys[job.createCompositeKey()] = struct{}{}
	}
	now := time.Now()
	cutoff := now.Add(-48 * time.Hour)
	var toBump, aged []DynamoDBItem
	for _, item := range seenJobs {
		if _, live := liveKeys[item.compositeKey()]; live {
			toBump = append(toBump, item)
		} else if item.LastSeen.Before(cutoff) && !item.HasApplied {
			aged = append(aged, item)
		}
	}
	app.Logger.Info("updating 'last_seen' for active entries", "count", len(toBump))
	if err := bumpLastSeen(ctx, app, toBump, now); err != nil {
		app.Logger.Error("cannot update last seen", "err", err)
	}
	app.Logger.Info("deleting aged out entries", "count", len(aged))
	if _, err := deleteAged(ctx, app, aged); err != nil {
		app.Logger.Error("cannot delete aged entries", "err", err)
	}
	app.Logger.Debug("checking again seen set ends", "time", time.Now())

	filter, err := LoadKeywordFilter(ctx, app)
	if err != nil {
		app.Logger.Error("cannot load filtering data ", "err", err)
		return fmt.Errorf("error loading filter file: %w", err)
	}
	matched := filterJobs(fresh, filter)
	app.Logger.Info("matched jobs", "count", len(matched))

	limiter := time.NewTicker(5 * time.Second) // ~12 req/min: will need to adjust if you change Gemini model used
	defer limiter.Stop()
	const batchSize = 5 // will need to adjust if you change Gemini model used
	var ranked []RankedJob
	throttle := &tpmThrottle{
		budget: 200000, // might need to adjust if you change Gemini model used
		window: 60 * time.Second,
	}
	for i := 0; i < len(matched); i += batchSize {
		<-limiter.C // RPM limiter
		end := min(i+batchSize, len(matched))

		tokenEstimate := 3000.0 // initial for estimated prompt/resume tokens
		var descChars int
		for _, j := range matched[i:end] {
			descChars += len(j.Description)
		}
		tokenEstimate += float64(descChars) / float64(1.75)

		if err := throttle.reserve(ctx, tokenEstimate); err != nil {
			break
		}
		batch, tokens, err := app.scoreBatchRetry(ctx, matched[i:end])
		if err != nil {
			app.Logger.Error("aborting run, batch failed", "start", i, "err", err)
			break
		}
		if tokens > 0 {
			app.Logger.Info("token estimate calibration",
				"est", tokenEstimate,
				"actual_total", tokens,
				"length of descriptions", descChars,
				"chars_per_token", float64(descChars)/float64(tokens),
				"est_ratio", float64(tokenEstimate)/float64(tokens))
		} else {
			app.Logger.Warn("zero token count on success path- skipping calibration", "start", i)
		}
		throttle.record(tokens)
		ranked = append(ranked, batch...)
	}
	if err := writeLogs(ctx, app); err != nil {
		return fmt.Errorf("error writing logs: %w", err)
	}
	return writeResultsToDynamoDB(ctx, app, ranked)
}
