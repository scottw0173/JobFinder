package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type KeywordFilter struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

func LoadKeywordFilter(path string) (*KeywordFilter, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading filter file %q: %w", path, err)
	}
	var f KeywordFilter
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing filter file %q: %w", path, err)
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
