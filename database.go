package main

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

type DynamoDBItem struct {
	Stablekey string `json:"stablekey"`
	PostedAt  int64  `json:"posted_at"`
	Score     int    `json:"score"`
	Title     string `json:"title"`
	Company   string `json:"company"`
	Location  string `json:"location"`
	URL       string `json:"url"`
	Source    string `json:"source"`
}

func writeResultsToDynamoDB(ctx context.Context, a *App, jobs []RankedJob) error {
	for _, job := range jobs {
		item := DynamoDBItem{
			Stablekey: job.Stablekey,
			PostedAt:  job.PostedAt,
			Score:     job.Score,
			Title:     job.Title,
			Company:   job.Company,
			Location:  job.Location,
			URL:       job.URL,
			Source:    job.Source,
		}
		attribute, err := attributevalue.MarshalMap(item)
		if err != nil {
			a.Logger.Error("error",
				slog.String("failed to marshal item", err.Error()),
				slog.String("job", job.Key))
			continue
		}
		if _, err := a.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(a.dynamoTableName),
			Item:      attribute,
		}); err != nil {
			a.Logger.Error("error",
				slog.String("failed to write to dynamoDB", err.Error()),
				slog.String("job", job.Key))
			continue
		}
	}
	return nil
}
