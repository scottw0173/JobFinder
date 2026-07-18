package main

import "context"

// azureSecrets is a placeholder until the Azure subscription is live and
// keyless managed-identity/Entra auth is wired up (pairs with the Bicep work
// in migration step 7). No connection strings or keys belong in this file.
type azureSecrets struct{}

func newAzureSecrets() *azureSecrets { return &azureSecrets{} }

func (s *azureSecrets) Fetch(ctx context.Context, name string) (string, error) {
	return "", traceErrorf("azure secrets require managed identity - not available before the Azure subscription is live")
}
