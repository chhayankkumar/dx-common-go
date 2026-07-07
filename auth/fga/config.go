package fga

import "time"

// Config carries settings for the dx-authz-go client.
type Config struct {
	// BaseURL is the root URL of dx-authz-go, e.g. "http://dx-authz-go:8080".
	BaseURL string `mapstructure:"base_url"`
	// Timeout for individual requests. Defaults to 2 seconds if zero.
	Timeout time.Duration `mapstructure:"timeout"`
	// ServiceToken is an optional bearer token sent on each request for
	// service-to-service authentication (when the authz service requires it).
	ServiceToken string `mapstructure:"service_token"`

	// SharedSecret enables HMAC service identity: each request carries
	// X-Subject-* headers for "svc:<ServiceName>" signed with this secret —
	// the same scheme the gateway uses for user identity, so dx-authz-go can
	// protect /v1/* with the standard resolver middleware. Empty disables it.
	SharedSecret string `mapstructure:"shared_secret"`
	// ServiceName identifies the caller in the signed identity
	// (e.g. "gateway", "files-connect"). Required when SharedSecret is set.
	ServiceName string `mapstructure:"service_name"`
}
