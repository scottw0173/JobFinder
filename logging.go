package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type closeFunc func() error

func initLogger() (*slog.Logger, *bytes.Buffer, error) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	return logger, &logBuf, nil
}

func writeLogs(ctx context.Context, a *App, logs []byte) error {
	identifier := fmt.Sprintf("logs-%s.json", time.Now().Format("2006-01-02T15-04-05"))
	_, err := a.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.s3Logs),
		Key:         aws.String(identifier),
		Body:        bytes.NewReader(logs),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to write logs to S3: %w", err)
	}
	return nil
}
