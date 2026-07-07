// Package sts vends short-lived, scoped credentials for direct client access to
// an S3-compatible backend, via AWS STS AssumeRole. It is a sibling of the s3
// package (not part of it) so services that only move objects never pull the
// STS SDK into their build.
//
// The mechanism is generic and business-free: the caller supplies the role and
// an IAM policy (Policy helpers build the common "scope to a key prefix" cases),
// and receives temporary Credentials. What a prefix means, and who may assume
// what, are the caller's concern.
package sts
