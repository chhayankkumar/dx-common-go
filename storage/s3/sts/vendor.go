package sts

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awssts "github.com/aws/aws-sdk-go-v2/service/sts"
)

// Config holds the STS connection settings. The static credentials authenticate
// the AssumeRole call itself; the returned Credentials are what a client uses.
type Config struct {
	Region          string `mapstructure:"region"`
	Endpoint        string `mapstructure:"endpoint"`
	AccessKeyID     string `mapstructure:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key"`
}

// Credentials are short-lived, scoped credentials returned by AssumeRole.
type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Expiration      time.Time
}

// Request parameterises one AssumeRole call.
type Request struct {
	// RoleARN is the role to assume.
	RoleARN string
	// SessionName identifies the session in CloudTrail (defaults applied by the
	// caller; STS requires a non-empty value).
	SessionName string
	// Duration bounds the credential lifetime. Zero means one hour.
	Duration time.Duration
	// Policy is an inline IAM policy (JSON) that further restricts the session —
	// build it with the Policy helpers in this package. Empty means the role's
	// own policy applies unchanged.
	Policy string
}

// Vendor issues temporary credentials. Safe for concurrent use.
type Vendor struct {
	client *awssts.Client
}

// NewVendor constructs a Vendor from cfg.
func NewVendor(cfg Config) (*Vendor, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("sts.NewVendor: load config: %w", err)
	}

	client := awssts.NewFromConfig(awsCfg, func(o *awssts.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return &Vendor{client: client}, nil
}

// AssumeRole returns temporary credentials for req.
func (v *Vendor) AssumeRole(ctx context.Context, req Request) (*Credentials, error) {
	if req.RoleARN == "" || req.SessionName == "" {
		return nil, fmt.Errorf("sts.AssumeRole: RoleARN and SessionName are required")
	}
	duration := int32((req.Duration).Seconds())
	if duration <= 0 {
		duration = 3600
	}

	in := &awssts.AssumeRoleInput{
		RoleArn:         aws.String(req.RoleARN),
		RoleSessionName: aws.String(req.SessionName),
		DurationSeconds: aws.Int32(duration),
	}
	if req.Policy != "" {
		in.Policy = aws.String(req.Policy)
	}

	out, err := v.client.AssumeRole(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("sts.AssumeRole: %w", err)
	}
	c := out.Credentials
	return &Credentials{
		AccessKeyID:     aws.ToString(c.AccessKeyId),
		SecretAccessKey: aws.ToString(c.SecretAccessKey),
		SessionToken:    aws.ToString(c.SessionToken),
		Expiration:      aws.ToTime(c.Expiration),
	}, nil
}
