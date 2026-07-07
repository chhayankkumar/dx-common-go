package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// ServiceOptions configures LoadService, capturing the viper boilerplate
// (config-file discovery, defaults, env binding) that every CDPG service
// otherwise repeats in its own config.Load.
type ServiceOptions struct {
	// ConfigName is the base file name to search for (default "config").
	ConfigName string
	// ConfigType is the file format (default "yaml").
	ConfigType string
	// Paths are the directories searched for the config file. Defaults to
	// [".", "./configs", "/app/configs"].
	Paths []string
	// Defaults are applied before reading the file/env (key → value).
	Defaults map[string]any
	// EnvPrefix, when set, scopes environment variables (e.g. "DX" →
	// DX_SERVER_PORT). Leave empty to bind unprefixed env vars.
	EnvPrefix string
}

// LoadService reads configuration into a value of type T using the standard
// CDPG conventions: optional config file (missing is not fatal — defaults and
// env take over), "." → "_" env-key replacement, and AutomaticEnv binding.
//
//	cfg, err := config.LoadService[Config](config.ServiceOptions{
//	    Defaults: map[string]any{"server.port": "8080", "jwt.enabled": false},
//	})
func LoadService[T any](opts ServiceOptions) (*T, error) {
	v := viper.New()

	name := opts.ConfigName
	if name == "" {
		name = "config"
	}
	typ := opts.ConfigType
	if typ == "" {
		typ = "yaml"
	}
	v.SetConfigName(name)
	v.SetConfigType(typ)

	paths := opts.Paths
	if len(paths) == 0 {
		paths = []string{".", "./configs", "/app/configs"}
	}
	for _, p := range paths {
		v.AddConfigPath(p)
	}

	for key, val := range opts.Defaults {
		v.SetDefault(key, val)
	}

	if opts.EnvPrefix != "" {
		v.SetEnvPrefix(opts.EnvPrefix)
	}
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file is optional: defaults + env are sufficient for many services.
	_ = v.ReadInConfig()

	var cfg T
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config.LoadService: unmarshal: %w", err)
	}
	return &cfg, nil
}
