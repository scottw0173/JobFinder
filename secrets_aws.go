package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// awsSecrets fetches secrets from SSM Parameter Store (SecureString, decrypted).
type awsSecrets struct {
	client *ssm.Client
}

func newAWSSecrets(client *ssm.Client) *awsSecrets {
	return &awsSecrets{client: client}
}

func (s *awsSecrets) Fetch(ctx context.Context, name string) (string, error) {
	out, err := s.client.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &name,
		WithDecryption: aws.Bool(true), // SecureString -> decrypt
	})
	if err != nil {
		return "", wrapErr(fmt.Sprintf("ssm get %s", name), err)
	}
	return *out.Parameter.Value, nil
}
