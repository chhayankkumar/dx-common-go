package sts

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Common S3 actions, provided so callers don't hand-type action strings.
var (
	ReadActions      = []string{"s3:GetObject"}
	ReadWriteActions = []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"}
)

// PrefixReadOnlyPolicy builds an inline IAM policy granting read-only access to
// objects under prefix in bucket (plus listing that prefix) — the common
// "hand a client a scoped, download-only session" case. Pass the result as
// Request.Policy.
func PrefixReadOnlyPolicy(bucket, prefix string) (string, error) {
	return PrefixPolicy(bucket, prefix, ReadActions)
}

// PrefixPolicy builds an inline IAM policy granting objectActions on objects
// under prefix in bucket, plus s3:ListBucket restricted to that prefix. prefix
// is normalised to end with "/". objectActions must be non-empty.
func PrefixPolicy(bucket, prefix string, objectActions []string) (string, error) {
	if bucket == "" {
		return "", fmt.Errorf("sts.PrefixPolicy: bucket is required")
	}
	if len(objectActions) == 0 {
		return "", fmt.Errorf("sts.PrefixPolicy: at least one object action is required")
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"

	policy := map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{
			{
				"Effect":   "Allow",
				"Action":   objectActions,
				"Resource": []string{fmt.Sprintf("arn:aws:s3:::%s/%s*", bucket, prefix)},
			},
			{
				"Effect":   "Allow",
				"Action":   []string{"s3:ListBucket"},
				"Resource": []string{fmt.Sprintf("arn:aws:s3:::%s", bucket)},
				"Condition": map[string]any{
					"StringLike": map[string]any{"s3:prefix": []string{prefix + "*"}},
				},
			},
		},
	}

	b, err := json.Marshal(policy)
	if err != nil {
		return "", fmt.Errorf("sts.PrefixPolicy: marshal: %w", err)
	}
	return string(b), nil
}
