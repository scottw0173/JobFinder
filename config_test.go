package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAWSConfigSourceDefaults(t *testing.T) {
	c := newAWSConfigSource(nil, "bucket", "gemini-3.1-flash-lite")
	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "gemini-3.1-flash-lite" {
		t.Fatalf("got %+v, want a single gemini-3.1-flash-lite model", models)
	}
	if c.RescoreEveryRun() {
		t.Fatal("AWS RescoreEveryRun() should be false")
	}
}

func TestAzureConfigSourceDefaults(t *testing.T) {
	t.Setenv("AZURE_MODELS", "")
	t.Setenv("AZURE_RESCORE_EVERY_RUN", "")
	c := newAzureConfigSource()

	models, err := c.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error: %v", err)
	}
	if len(models) < 2 {
		t.Fatalf("got %d models, want a multi-model default spread", len(models))
	}
	if !c.RescoreEveryRun() {
		t.Fatal("azure RescoreEveryRun() should default true")
	}
}

func TestAzureConfigSourceReadsLocalFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "filterKeywords.json"), []byte(`{"include":[],"exclude":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AZURE_CONFIG_DIR", dir)
	c := newAzureConfigSource()

	data, err := c.File(context.Background(), "filterKeywords.json")
	if err != nil {
		t.Fatalf("File() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty file content")
	}
}
