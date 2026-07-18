package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type DynamoDBItem struct {
	Stablekey  string    `dynamodbav:"stablekey"`
	PostedAt   int64     `dynamodbav:"posted_at"`
	Title      string    `dynamodbav:"title"`
	Company    string    `dynamodbav:"company"`
	Score      int       `dynamodbav:"score"`
	Reasoning  string    `dynamodbav:"reasoning"`
	Location   string    `dynamodbav:"location"`
	URL        string    `dynamodbav:"url"`
	Source     string    `dynamodbav:"source"`
	HasApplied bool      `dynamodbav:"has_applied"`
	LastSeen   time.Time `dynamodbav:"last_seen"`
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
				LastSeen:  time.Now(),
			})
			if err != nil {
				a.Logger.Error("failed to marshal item", errAttr(err), slog.String("job", job.Key))
				continue
			}
			reqs = append(reqs, types.WriteRequest{PutRequest: &types.PutRequest{Item: item}})
		}
		if _, err := a.dynamoClient.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{a.dynamoTableName: reqs},
		}); err != nil {
			a.Logger.Error("failed to write batch", errAttr(err))
			continue
		}
	}
	return nil
}

func scanAllJobs(ctx context.Context, a *App) ([]DynamoDBItem, error) {
	var items []DynamoDBItem
	p := dynamodb.NewScanPaginator(a.dynamoClient, &dynamodb.ScanInput{
		TableName:            &a.dynamoTableName,
		ProjectionExpression: aws.String("stablekey, posted_at, has_applied, last_seen"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, wrapErr("error scanning jobs table", err)
		}
		var batch []DynamoDBItem
		if err := attributevalue.UnmarshalListOfMaps(page.Items, &batch); err != nil {
			return nil, wrapErr("error unmarshalling scan page", err)
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

func compositeKey(stablekey string, postedAt int64) string {
	return fmt.Sprintf("%s#%d", stablekey, postedAt)
}

func (it DynamoDBItem) compositeKey() string {
	return compositeKey(it.Stablekey, it.PostedAt)
}

func bumpLastSeen(ctx context.Context, app *App, items []DynamoDBItem, now time.Time) error {
	nowStr := now.UTC().Format(time.RFC3339Nano)
	for _, it := range items {
		_, err := app.dynamoClient.UpdateItem(ctx, &dynamodb.UpdateItemInput{
			TableName:        aws.String(app.dynamoTableName),
			Key:              itemKey(it),
			UpdateExpression: aws.String("SET last_seen = :n"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":n": &types.AttributeValueMemberS{Value: nowStr},
			},
		})
		if err != nil {
			return wrapErr(fmt.Sprintf("bump %s", it.compositeKey()), err)
		}
	}
	return nil
}

func deleteAged(ctx context.Context, app *App, items []DynamoDBItem) (int, error) {
	deleted := 0
	for i := 0; i < len(items); i += 25 {
		end := min(i+25, len(items))
		reqs := make([]types.WriteRequest, 0, end-i)
		for _, it := range items[i:end] {
			reqs = append(reqs, types.WriteRequest{
				DeleteRequest: &types.DeleteRequest{Key: itemKey(it)},
			})
		}
		out, err := app.dynamoClient.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{app.dynamoTableName: reqs},
		})
		if err != nil {
			return deleted, wrapErr(fmt.Sprintf("delete batch at %d", i), err)
		}
		deleted += (end - i) - len(out.UnprocessedItems[app.dynamoTableName])
	}
	return deleted, nil
}

func itemKey(it DynamoDBItem) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{
		"stablekey": &types.AttributeValueMemberS{Value: it.Stablekey},
		"posted_at": &types.AttributeValueMemberN{Value: strconv.FormatInt(it.PostedAt, 10)},
	}
}
