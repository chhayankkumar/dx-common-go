package config

import (
	"os"
	"path/filepath"
	"testing"
)

type testCfg struct {
	Server struct {
		Port string `mapstructure:"port"`
		Host string `mapstructure:"host"`
	} `mapstructure:"server"`
	Debug bool `mapstructure:"debug"`
}

func TestLoadServiceDefaults(t *testing.T) {
	cfg, err := LoadService[testCfg](ServiceOptions{
		Paths: []string{t.TempDir()}, // no config file present
		Defaults: map[string]any{
			"server.port": "8080",
			"server.host": "localhost",
			"debug":       true,
		},
	})
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if cfg.Server.Port != "8080" || cfg.Server.Host != "localhost" || !cfg.Debug {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadServiceEnvOverride(t *testing.T) {
	t.Setenv("SERVER_PORT", "9090")
	cfg, err := LoadService[testCfg](ServiceOptions{
		Paths:    []string{t.TempDir()},
		Defaults: map[string]any{"server.port": "8080"},
	})
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if cfg.Server.Port != "9090" {
		t.Fatalf("env override failed: port = %q, want 9090", cfg.Server.Port)
	}
}

func TestLoadServiceEnvPrefix(t *testing.T) {
	t.Setenv("DX_DEBUG", "true")
	// Unprefixed DEBUG must be ignored when a prefix is set.
	t.Setenv("DEBUG", "false")
	cfg, err := LoadService[testCfg](ServiceOptions{
		EnvPrefix: "DX",
		Paths:     []string{t.TempDir()},
		Defaults:  map[string]any{"debug": false},
	})
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if !cfg.Debug {
		t.Fatalf("prefixed env not applied: debug = %v, want true", cfg.Debug)
	}
}

func TestLoadServiceReadsFile(t *testing.T) {
	dir := t.TempDir()
	yaml := "server:\n  port: \"7000\"\n  host: fromfile\ndebug: true\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadService[testCfg](ServiceOptions{Paths: []string{dir}})
	if err != nil {
		t.Fatalf("LoadService: %v", err)
	}
	if cfg.Server.Port != "7000" || cfg.Server.Host != "fromfile" || !cfg.Debug {
		t.Fatalf("config file not read: %+v", cfg)
	}
}

func TestLoadServiceMissingFileNotFatal(t *testing.T) {
	// A directory with no config file must not error — defaults/env suffice.
	if _, err := LoadService[testCfg](ServiceOptions{Paths: []string{t.TempDir()}}); err != nil {
		t.Fatalf("missing config file should not be fatal: %v", err)
	}
}
