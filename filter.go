package main

import (
	"context"
	"encoding/json"
	"strings"
)

type KeywordFilter struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

func LoadKeywordFilter(ctx context.Context, a *App) (*KeywordFilter, error) {
	data, err := a.Config.File(ctx, "filterKeywords.json")
	if err != nil {
		wrapped := wrapErr("reading filter file", err)
		a.Logger.Error("error loading keyword filter", errAttr(wrapped))
		return nil, wrapped
	}
	var f KeywordFilter
	if err := json.Unmarshal(data, &f); err != nil {
		wrapped := wrapErr("parsing filter file", err)
		a.Logger.Error("error unmarshalling keyword filter", errAttr(wrapped))
		return nil, wrapped
	}
	return &f, nil
}

// Matches reports whether text passes the filter: it must contain at least
// one Include word (unless Include is empty) and no Exclude words.
func (f *KeywordFilter) Matches(text string) bool {
	text = strings.ToLower(text)

	for _, w := range f.Exclude {
		if strings.Contains(text, strings.ToLower(w)) {
			return false
		}
	}

	if len(f.Include) == 0 {
		return true
	}
	for _, w := range f.Include {
		if strings.Contains(text, strings.ToLower(w)) {
			return true
		}
	}
	return false
}
