package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client is an S3-compatible object store (AWS S3 or MinIO) built on the AWS
// SDK v2. It implements ObjectStore and is safe for concurrent use.
type Client struct {
	api     *awss3.Client
	presign *awss3.PresignClient
	bucket  string
}

// NewClient connects to the bucket in cfg. For MinIO, set Endpoint and
// ForcePathStyle=true. Endpoint may be a bare host[:port] (the scheme is taken
// from UseSSL) or a full URL.
func NewClient(cfg Config) (*Client, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3.NewClient: bucket is required")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("s3.NewClient: load config: %w", err)
	}

	api := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if endpoint := normalizeEndpoint(cfg.Endpoint, cfg.UseSSL); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &Client{
		api:     api,
		presign: awss3.NewPresignClient(api),
		bucket:  cfg.Bucket,
	}, nil
}

// Bucket returns the bucket every operation targets.
func (c *Client) Bucket() string { return c.bucket }

// normalizeEndpoint returns "" for the AWS default, or a full URL: an endpoint
// that already carries a scheme is used verbatim, otherwise the scheme is
// derived from useSSL.
func normalizeEndpoint(endpoint string, useSSL bool) string {
	if endpoint == "" {
		return ""
	}
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	return scheme + "://" + endpoint
}
