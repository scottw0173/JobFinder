package main

import (
	"testing"
)

func TestBuildRows(t *testing.T) {
	items := []DynamoDBItem{{Stablekey: "gh-123", PostedAt: 1735689600, Title: "TSE", Company: "Acme", Score: 82}}
	rows := buildRows(items)
	if got := rows[1][2]; got != "2025-01-01" {
		t.Fatalf("posted date = %v, want 2025-01-01", got)
	}
}
