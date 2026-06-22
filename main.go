package main

import (
	"net/http"
	"time"
)

// Package-level HTTP client.
// Go's http.Client is completely safe for concurrent use by multiple goroutines.
var httpClient = &http.Client{
	Timeout: 10 * time.Second, // Always specify a sane timeout
}
