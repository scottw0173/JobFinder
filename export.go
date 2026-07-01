package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

func scanAllJobs(ctx context.Context, a *App) ([]DynamoDBItem, error) {
	var items []DynamoDBItem
	p := dynamodb.NewScanPaginator(a.dynamoClient, &dynamodb.ScanInput{
		TableName:            &a.dynamoTableName,
		ProjectionExpression: aws.String("stablekey, posted_at, has_applied, last_seen"),
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
