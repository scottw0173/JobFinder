package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
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
				Stablekey: job.createStableKey(),
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

func scanAllJobs(ctx context.Context, a *App) ([]DynamoDBItem, error) {
	var items []DynamoDBItem
	p := dynamodb.NewScanPaginator(a.dynamoClient, &dynamodb.ScanInput{
		TableName:            &a.dynamoTableName,
		ProjectionExpression: aws.String("stablekey, posted_at, has_applied"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("error scanning jobs table: %w", err)
		}
		var batch []DynamoDBItem
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &batch); err != nil {
			return nil, fmt.Errorf("error unmarshalling scan page: %w", err)
		}
		items = append(items, batch...)
	}
	return items, nil
}

func compositeKeySet(items []DynamoDBItem) map[string]bool {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		seen[it.compositeKey()] = true
	}
	return seen
}
