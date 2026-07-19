package main

import (
	"context"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	Logger *slog.Logger
	Client *http.Client // shared, cloud-agnostic (ATS fetchers)

	Store   Store
	Config  ConfigSource
	Secrets Secrets
	Scorer  Scorer

	spreadsheetID string // Sheets export stays outside the four interfaces

	cloudProvider string
}

var app *App

func main() {
	provider := os.Getenv("CLOUD_PROVIDER")
	if provider == "" {
		provider = "aws"
	}

	logger := initLogger()

	app = &App{
		Logger:        logger,
		Client:        &http.Client{Timeout: 60 * time.Second},
		cloudProvider: provider,
	}

	ctx := context.Background()

	var err error
	switch provider {
	case "aws":
		err = wireAWS(ctx, app)
	case "azure":
		err = wireAzure(ctx, app)
	default:
		log.Fatalf("unknown CLOUD_PROVIDER %q", provider)
	}
	if err != nil {
		log.Fatalf("error wiring %s: %v", provider, err)
	}

	// AWS_LAMBDA_RUNTIME_API is set by the Lambda platform itself, so this
	// detects "am I running inside Lambda" independent of CLOUD_PROVIDER.
	// Lambda's provided.al2023 custom runtime requires the process to speak
	// the Runtime API loop (lambda.Start handles that); everywhere else
	// (Azure Container Apps Jobs, local dev) this is a single plain run.
	if os.Getenv("AWS_LAMBDA_RUNTIME_API") != "" {
		lambda.Start(func(ctx context.Context) error {
			if err := handler(ctx); err != nil {
				app.Logger.Error("AWS run failed", errAttr(err))
				return err
			}
			return nil
		})
		return
	}

	if err := handler(ctx); err != nil {
		app.Logger.Error("Azure run failed", errAttr(err))
		os.Exit(1)
	}
}

// wireAWS builds the AWS-backed Store/ConfigSource/Secrets/Scorer and wires
// them onto app, preserving the exact env vars and startup order the AWS
// Lambda deployment has always used.
func wireAWS(ctx context.Context, app *App) error {
	dynamoTableName := os.Getenv("DYNAMOTABLE")
	if dynamoTableName == "" {
		return traceErrorf("DYNAMOTABLE env var not set")
	}
	s3Region := os.Getenv("S3REGION")
	if s3Region == "" {
		return traceErrorf("S3REGION env var not set")
	}
	geminimodel := os.Getenv("GEMINIMODEL")
	if geminimodel == "" {
		return traceErrorf("GEMINIMODEL env var not set")
	}
	s3config := os.Getenv("S3CONFIG")
	if s3config == "" {
		return traceErrorf("S3CONFIG env var not set")
	}
	app.spreadsheetID = os.Getenv("SPREADSHEETID")

	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(s3Region))
	if err != nil {
		return wrapErr("configuring aws sdk", err)
	}

	dynamoClient := dynamodb.NewFromConfig(awsCfg)
	s3client := s3.NewFromConfig(awsCfg)
	ssmClient := ssm.NewFromConfig(awsCfg)

	app.Store = newAWSStore(app.Logger, dynamoClient, dynamoTableName)
	app.Config = newAWSConfigSource(s3client, s3config, geminimodel)
	app.Secrets = newAWSSecrets(ssmClient)

	geminikey, err := app.Secrets.Fetch(ctx, os.Getenv("GEMINIAPIKEY"))
	if err != nil {
		return wrapErr("fetching gemini key", err)
	}

	app.Logger.Info("app and logger initialized", "time", time.Now())
	instructions, err := app.Config.File(ctx, "instructions.md")
	if err != nil {
		return wrapErr("getting instructions", err)
	}
	app.Scorer = newGeminiScorer(app.Logger, app.Client, geminikey, instructions)
	return nil
}

// wireAzure builds the Azure-side impls. Most are stubs until their
// corresponding later migration steps land (see store_azure.go,
// secrets_azure.go, scorer_azure.go); ConfigSource is functional now since
// handler() needs it to succeed at startup.
func wireAzure(ctx context.Context, app *App) error {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		return traceErrorf("POSTGRES_DSN env var not set")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return wrapErr("connecting to postgres", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return wrapErr("pinging postgres", err)
	}

	app.Store = newAzureStore(app.Logger, pool)
	app.Config = newAzureConfigSource()
	app.Secrets = newAzureSecrets()

	app.Logger.Info("app and logger initialized", "time", time.Now())
	instructions, err := app.Config.File(ctx, "instructions.md")
	if err != nil {
		return wrapErr("getting instructions", err)
	}

	baseURL := os.Getenv("AZURE_OPENAI_ENDPOINT")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1" // Ollama's well-known default port; safe local-dev default
	}
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY") // empty is expected/fine for local Ollama; real key lands in step 7

	// Dedicated client, not app.Client: that one's tuned for fast ATS API
	// fetches (60s timeout). Local CPU inference on a 5-job batch can
	// legitimately take longer than that; a hung real endpoint should still
	// fail eventually, just not on the ATS fetchers' clock.
	scorerClient := &http.Client{Timeout: 5 * time.Minute}
	app.Scorer = newAzureScorer(app.Logger, scorerClient, baseURL, apiKey, instructions)
	return nil
}

func handler(ctx context.Context) error {
	all, err := collect(ctx, app)
	if err != nil {
		wrapped := wrapErr("error collecting jobs", err)
		app.Logger.Error("cannot collect jobs", errAttr(wrapped))
		return wrapped
	}

	seenJobs, err := app.Store.SeenJobs(ctx)
	if err != nil {
		app.Logger.Error("cannot read results from store", errAttr(err))
	}
	seenSet := seenJobKeySet(seenJobs)
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
	var toBump, aged []SeenJob
	for _, item := range seenJobs {
		if _, live := liveKeys[item.compositeKey()]; live {
			toBump = append(toBump, item)
		} else if item.LastSeen.Before(cutoff) && !item.HasApplied {
			aged = append(aged, item)
		}
	}
	app.Logger.Info("updating 'last_seen' for active entries", "count", len(toBump))
	if err := app.Store.BumpLastSeen(ctx, toBump, now); err != nil {
		app.Logger.Error("cannot update last seen", errAttr(err))
	}
	app.Logger.Info("deleting aged out entries", "count", len(aged))
	if _, err := app.Store.DeleteAged(ctx, aged); err != nil {
		app.Logger.Error("cannot delete aged entries", errAttr(err))
	}
	app.Logger.Debug("checking again seen set ends", "time", time.Now())

	filter, err := LoadKeywordFilter(ctx, app)
	if err != nil {
		wrapped := wrapErr("error loading filter file", err)
		app.Logger.Error("cannot load filtering data ", errAttr(wrapped))
		return wrapped
	}

	// Rescore policy is configuration, not forked code: AWS skips jobs it has
	// already scored (fresh only); Azure re-scores every currently-live job
	// each run to measure cross-model/temporal drift.
	candidates := fresh
	if app.Config.RescoreEveryRun() {
		candidates = all
	}
	matched := filterJobs(candidates, filter)
	app.Logger.Info("matched jobs", "count", len(matched))

	models, err := app.Config.Models(ctx)
	if err != nil {
		wrapped := wrapErr("error loading model list", err)
		app.Logger.Error("cannot load model list", errAttr(wrapped))
		return wrapped
	}

	limiter := time.NewTicker(5 * time.Second) // ~12 req/min: will need to adjust if you change Gemini model used
	defer limiter.Stop()
	const batchSize = 5 // will need to adjust if you change Gemini model used
	var events []ScoringEvent
	for _, model := range models {
		// Fresh throttle per model: the token budget below is tuned for
		// Gemini's free tier, not a shared ceiling across every model in
		// Azure's list, each of which has its own independent rate limit.
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
			results, tokens, err := app.scoreBatchRetry(ctx, matched[i:end], model)
			if err != nil {
				app.Logger.Error("aborting run, batch failed", "start", i, "model", model.Name, errAttr(err))
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
			events = append(events, zipScoreEvents(app, matched[i:end], results)...)
		}
	}
	if err := app.Store.RecordScores(ctx, events); err != nil {
		app.Logger.Error("cannot write results to store", errAttr(err))
	} else {
		app.Logger.Info("results successfully written to store")
	}
	rows, err := app.Store.ExportRows(ctx)
	if err != nil {
		app.Logger.Error("cannot gather jobs for export", errAttr(err))
	} else if err := exportToSheet(ctx, app, rows); err != nil {
		app.Logger.Error("cannot export jobs to sheet", errAttr(err))
	}
	return nil
}
