package main

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
	Reasoning string `json:"reasoning"`
}

func writeResultsToDynamoDB(ctx context.Context, a *App, jobs []RankedJob) {
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
}
