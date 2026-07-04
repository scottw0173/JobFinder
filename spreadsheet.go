package main

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func gatherJobsForExport(ctx context.Context, a *App) ([]DynamoDBItem, error) {
	var items []DynamoDBItem
	p := dynamodb.NewScanPaginator(a.dynamoClient, &dynamodb.ScanInput{
		TableName:            &a.dynamoTableName,
		ProjectionExpression: aws.String("stablekey, posted_at, has_applied, last_seen, title, company, score"),
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

func newSheetsService(ctx context.Context, a *App) (*sheets.Service, error) {
	out, err := fetchSecret(ctx, a, "GCP-Project-Key")
	if err != nil {
		return nil, fmt.Errorf("fetching SA key: %w", err)
	}
	return sheets.NewService(ctx,
		option.WithCredentialsJSON([]byte(out)),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
}

func buildRows(items []DynamoDBItem) [][]interface{} {
	rows := [][]interface{}{{"stablekey", "posted_at", "posted", "title", "company", "score", "has_applied"}}
	for _, it := range items {
		posted := time.Unix(it.PostedAt, 0).UTC().Format("2006-01-02")
		rows = append(rows, []interface{}{it.Stablekey, it.PostedAt, posted, it.Title, it.Company, it.Score, it.HasApplied})
	}
	return rows
}

func exportToSheet(ctx context.Context, a *App, items []DynamoDBItem) error {
	svc, err := newSheetsService(ctx, a)
	if err != nil {
		return err
	}

	rows := buildRows(items)

	if _, err := svc.Spreadsheets.Values.Clear(a.spreadsheetID, "Jobs!A:G",
		&sheets.ClearValuesRequest{}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("clearing sheet: %w", err)
	}
	_, err = svc.Spreadsheets.Values.Update(a.spreadsheetID, "Jobs!A1",
		&sheets.ValueRange{Values: rows}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("writing sheet: %w", err)
	}
	return nil
}
