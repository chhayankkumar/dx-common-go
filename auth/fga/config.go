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
}
