package main

import (
	"context"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

func newSheetsService(ctx context.Context, a *App) (*sheets.Service, error) {
	out, err := a.Secrets.Fetch(ctx, "GCP-Project-Key")
	if err != nil {
		return nil, wrapErr("fetching SA key", err)
	}
	return sheets.NewService(ctx,
		option.WithCredentialsJSON([]byte(out)),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
}

func buildRows(rows []ExportRow) [][]interface{} {
	out := [][]interface{}{{"stablekey", "posted_at", "posted", "title", "company", "score", "has_applied", "reasoning", "location", "url"}}
	for _, r := range rows {
		posted := time.Unix(r.PostedAt, 0).UTC().Format("2006-01-02")
		out = append(out, []interface{}{r.Stablekey, r.PostedAt, posted, r.Title, r.Company, r.Score, r.HasApplied, r.Reasoning, r.Location, r.URL})
	}
	return out
}

func exportToSheet(ctx context.Context, a *App, rows []ExportRow) error {
	svc, err := newSheetsService(ctx, a)
	if err != nil {
		return err
	}

	values := buildRows(rows)

	if _, err := svc.Spreadsheets.Values.Clear(a.spreadsheetID, "Jobs!A:J",
		&sheets.ClearValuesRequest{}).Context(ctx).Do(); err != nil {
		return wrapErr("clearing sheet", err)
	}
	_, err = svc.Spreadsheets.Values.Update(a.spreadsheetID, "Jobs!A1",
		&sheets.ValueRange{Values: values}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return wrapErr("writing sheet", err)
	}
	return nil
}
