package s3

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// PresignGetURL returns a time-limited URL a client can GET directly.
func (c *Client) PresignGetURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignGetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, awss3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3.PresignGetURL %q: %w", key, err)
	}
	return req.URL, nil
}

// PresignPutURL returns a time-limited URL a client can PUT directly. A
// non-empty contentType binds the upload's Content-Type.
func (c *Client) PresignPutURL(ctx context.Context, key, contentType string, expiry time.Duration) (string, error) {
	in := &awss3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	req, err := c.presign.PresignPutObject(ctx, in, awss3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3.PresignPutURL %q: %w", key, err)
	}
	return req.URL, nil
}

// PresignUploadPart returns a time-limited URL for uploading one part of an
// in-progress multipart upload — the browser-driven multipart path, where the
// client PUTs each part directly and the service only initiates and completes.
func (c *Client) PresignUploadPart(ctx context.Context, key, uploadID string, partNumber int32, expiry time.Duration) (string, error) {
	req, err := c.presign.PresignUploadPart(ctx, &awss3.UploadPartInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
	}, awss3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("s3.PresignUploadPart %q part %d: %w", key, partNumber, err)
	}
	return req.URL, nil
}
