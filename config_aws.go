package main

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// awsConfigSource reads file-shaped config from S3 and reports the AWS-side
// policy: score once, skip already-scored jobs, a single Gemini model.
type awsConfigSource struct {
	client    *s3.Client
	bucket    string
	modelName string
}

func newAWSConfigSource(client *s3.Client, bucket, modelName string) *awsConfigSource {
	return &awsConfigSource{client: client, bucket: bucket, modelName: modelName}
}

func (c *awsConfigSource) File(ctx context.Context, name string) ([]byte, error) {
	output, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(name),
	})
	if err != nil {
		return nil, wrapErr(fmt.Sprintf("get s3 object %s", name), err)
	}
	defer output.Body.Close()
	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, wrapErr(fmt.Sprintf("read s3 object %s", name), err)
	}
	return data, nil
}

func (c *awsConfigSource) Models(ctx context.Context) ([]ModelConfig, error) {
	return []ModelConfig{{Name: c.modelName}}, nil
}

func (c *awsConfigSource) RescoreEveryRun() bool {
	return false
}

// Temperature is unused on the AWS path: geminiScorer never sends it (the
// Azure measurement instrument's §4.7 requirement doesn't apply here).
func (c *awsConfigSource) Temperature() float32 {
	return 0
}
