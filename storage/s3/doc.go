// Package s3 is the platform's object-storage client for any S3-compatible
// backend (AWS S3 or MinIO). The concrete *Client implements the ObjectStore
// contract; services depend on the interface and construct with NewClient.
//
// The package is organised by concern:
//
//	store.go      the ObjectStore contract + ObjectInfo/CompletedPart/MultipartUpload types
//	config.go     connection Config (S3 or MinIO)
//	client.go     Client construction + Bucket()
//	object.go     Put/Get/GetRange/Head/Exists/Delete/Copy
//	list.go       ListObjects (+ WithDelimiter)
//	presign.go    time-limited GET/PUT/UploadPart URLs
//	multipart.go  multipart-upload lifecycle
//
// It is deliberately business-free: keys are opaque, and layout/naming/policy
// belong to the caller. Short-lived scoped credentials (STS AssumeRole) live in
// the sibling sub-package s3/sts, which keeps the STS SDK out of this package's
// dependency graph.
package s3
