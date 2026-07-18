package main

import "context"

// Secrets retrieves a named secret. AWS: an SSM parameter name/ARN. Azure:
// managed identity via Entra, once live - this shape stays the same either
// way, so callers never change.
type Secrets interface {
	Fetch(ctx context.Context, name string) (string, error)
}
