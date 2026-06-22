package main

import (
	"encoding/json"
	"os"
)

func createSourcesMap(a *App) map[string][]string {

	data, err := os.ReadFile("sources.json")
	if err != nil {
		a.Logger.Error("cannot read sources.json", "err", err)
		os.Exit(1)
	}
	var sources map[string][]string
	if err := json.Unmarshal(data, &sources); err != nil {
		a.Logger.Error("cannot unmarshal sources.json", "err", err)
		os.Exit(1)
	}
	return sources
}
