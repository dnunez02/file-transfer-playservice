package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"example.com/go-file-transfer/transfer-server/filesource"
)

// Source reads files from an S3 bucket. The AWS SDK talks to S3 over HTTPS (TCP), never UDP.
type Source struct {
	client *awss3.Client
	bucket string
	prefix string
}

// NewSource builds a Source for bucket, with every key starting with prefix.
//
// Credentials and region come from the default AWS chain (environment, shared config, or
// the EC2 instance profile).
// Errors: returns an error when the AWS configuration cannot be loaded.
func NewSource(ctx context.Context, bucket string, prefix string) (*Source, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Source{client: awss3.NewFromConfig(cfg), bucket: bucket, prefix: prefix}, nil
}

// Fetch downloads the whole object named name into memory. Safe for concurrent use.
//
// Errors: returns an error for an invalid name, a missing object, or any S3 failure.
func (s *Source) Fetch(name string) ([]byte, error) {
	if !filesource.ValidName(name) {
		return nil, fmt.Errorf("invalid file name %q", name)
	}
	out, err := s.client.GetObject(context.Background(), &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.prefix + name),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}
