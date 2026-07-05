package redis

// Config holds settings for connecting to a Redis instance.
type Config struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`

	// EnableTracing instruments the client with OpenTelemetry (redisotel):
	// one span per command, reading OTel's global TracerProvider and
	// propagator that observability.Init configured. A no-op until a provider
	// is set, so it is safe to leave on.
	EnableTracing bool `mapstructure:"enable_tracing"`
}
