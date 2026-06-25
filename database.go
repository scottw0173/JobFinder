package main

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type DynamoDBItem struct {
	Stablekey  string `dynamodbav:"stablekey"`
	PostedAt   int64  `dynamodbav:"posted_at"`
	Title      string `dynamodbav:"title"`
	Company    string `dynamodbav:"company"`
	Score      int    `dynamodbav:"score"`
	Reasoning  string `dynamodbav:"reasoning"`
	Location   string `dynamodbav:"location"`
	URL        string `dynamodbav:"url"`
	Source     string `dynamodbav:"source"`
	HasApplied bool   `dynamodbav:"has_applied"`
}

func writeResultsToDynamoDB(ctx context.Context, a *App, jobs []RankedJob) error {
	const batchSize = 20
	for i := 0; i < len(jobs); i += batchSize {
		end := min(i+batchSize, len(jobs))
		reqs := make([]types.WriteRequest, 0, end-i)
		for _, job := range jobs[i:end] {
			item, err := attributevalue.MarshalMap(DynamoDBItem{
				Stablekey: job.Stablekey,
				PostedAt:  job.PostedAt,
				Score:     job.Score,
				Title:     job.Title,
				Company:   job.Company,
				Location:  job.Location,
				URL:       job.URL,
				Source:    job.Source,
				Reasoning: job.Reasoning,
			})
			if err != nil {
				a.Logger.Error("failed to marshal item", slog.String("err", err.Error()), slog.String("job", job.Key))
				continue
			}
			reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: item}})
		}
		if _, err := a.dynamoClient.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{a.dynamoTableName: reqs},
		}); err != nil {
			a.Logger.Error("failed to write batch", slog.String("err", err.Error()))
			continue
		}
	}
	return nil
}
