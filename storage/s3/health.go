package s3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// HealthCheck verifies connectivity and access to the configured bucket with a
// lightweight HeadBucket call. It satisfies the object-store pinger the
// health.NewObjectStoreChecker expects, so a readiness probe can include the
// object store the same way it includes Postgres or Redis.
func (c *Client) HealthCheck(ctx context.Context) error {
	if _, err := c.api.HeadBucket(ctx, &awss3.HeadBucketInput{
		Bucket: aws.String(c.bucket),
	}); err != nil {
		return fmt.Errorf("s3.HealthCheck bucket %q: %w", c.bucket, err)
	}
	return nil
}
