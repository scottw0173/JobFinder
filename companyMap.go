package main

import (
	"encoding/json"
	"os"
)

func createCompanyMap(a *App) map[string][]string {

	data, err := os.ReadFile("companies.json")
	if err != nil {
		a.Logger.Error("cannot read companies.json", "err", err)
		os.Exit(1)
	}
	var sources map[string][]string
	if err := json.Unmarshal(data, &sources); err != nil {
		a.Logger.Error("cannot unmarshal companies.json", "err", err)
		os.Exit(1)
	}
	return sources
}
