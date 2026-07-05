package s3

import (
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// InitiateMultipartUpload starts a multipart upload and returns its upload ID.
func (c *Client) InitiateMultipartUpload(ctx context.Context, key, contentType string) (string, error) {
	in := &awss3.CreateMultipartUploadInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	out, err := c.api.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", fmt.Errorf("s3.InitiateMultipartUpload %q: %w", key, err)
	}
	return aws.ToString(out.UploadId), nil
}

// UploadPart uploads one part (server-side path) and returns its ETag.
func (c *Client) UploadPart(ctx context.Context, key, uploadID string, partNumber int32, body io.Reader, size int64) (string, error) {
	in := &awss3.UploadPartInput{
		Bucket:     aws.String(c.bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(partNumber),
		Body:       body,
	}
	if size > 0 {
		in.ContentLength = aws.Int64(size)
	}
	out, err := c.api.UploadPart(ctx, in)
	if err != nil {
		return "", fmt.Errorf("s3.UploadPart %q part %d: %w", key, partNumber, err)
	}
	return aws.ToString(out.ETag), nil
}

// CompleteMultipartUpload assembles the uploaded parts into the final object.
func (c *Client) CompleteMultipartUpload(ctx context.Context, key, uploadID string, parts []CompletedPart) error {
	completed := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	if _, err := c.api.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(c.bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	}); err != nil {
		return fmt.Errorf("s3.CompleteMultipartUpload %q: %w", key, err)
	}
	return nil
}

// AbortMultipartUpload cancels an in-progress multipart upload.
func (c *Client) AbortMultipartUpload(ctx context.Context, key, uploadID string) error {
	if _, err := c.api.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	}); err != nil {
		return fmt.Errorf("s3.AbortMultipartUpload %q: %w", key, err)
	}
	return nil
}

// ListMultipartUploads returns all in-progress multipart uploads, paginating
// internally — used to reap uploads orphaned by clients that never completed.
func (c *Client) ListMultipartUploads(ctx context.Context) ([]MultipartUpload, error) {
	var uploads []MultipartUpload
	var keyMarker, uploadIDMarker *string
	for {
		out, err := c.api.ListMultipartUploads(ctx, &awss3.ListMultipartUploadsInput{
			Bucket:         aws.String(c.bucket),
			KeyMarker:      keyMarker,
			UploadIdMarker: uploadIDMarker,
		})
		if err != nil {
			return nil, fmt.Errorf("s3.ListMultipartUploads: %w", err)
		}
		for _, u := range out.Uploads {
			uploads = append(uploads, MultipartUpload{
				Key:       aws.ToString(u.Key),
				UploadID:  aws.ToString(u.UploadId),
				Initiated: aws.ToTime(u.Initiated),
			})
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		keyMarker, uploadIDMarker = out.NextKeyMarker, out.NextUploadIdMarker
	}
	return uploads, nil
}
