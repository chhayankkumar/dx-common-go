package client

import "time"

// Config holds the connection settings for a gRPC client channel.
type Config struct {
	// Target is the server address (host:port).
	Target string `mapstructure:"target"`
	// TLS enables transport TLS. When false (default) the channel is
	// insecure — appropriate when TLS is terminated by a service mesh.
	TLS bool `mapstructure:"tls"`
	// CACertPath, when set (and TLS is true), trusts a private-CA PEM bundle
	// instead of the system roots.
	CACertPath string `mapstructure:"ca_cert_path"`
	// ServerNameOverride overrides the TLS SNI/verification hostname (rarely
	// needed; for mesh setups where the dialed host differs from the cert CN).
	ServerNameOverride string `mapstructure:"server_name_override"`
	// KeepaliveTime pings an idle connection after this long to detect a
	// half-open channel. Zero uses a 30s default.
	KeepaliveTime time.Duration `mapstructure:"keepalive_time"`
	// KeepaliveTimeout bounds how long a keepalive ping waits for an ack
	// before the connection is considered dead. Zero uses a 10s default.
	KeepaliveTimeout time.Duration `mapstructure:"keepalive_timeout"`
}
