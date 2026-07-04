// Package client provides pgxpool connection management and configuration
// for PostgreSQL — connection pooling, pgx.QueryTracer composition
// (MultiTracer), and slow-query logging (SlowQueryTracer). Transaction
// helpers live in the sibling database/postgres/transaction package.
package client

import "time"

// Config holds connection pool settings for PostgreSQL via pgx.
type Config struct {
	// DSN is the full connection string, e.g.
	// postgres://user:pass@localhost:5433/dbname?sslmode=disable
	DSN             string        `mapstructure:"dsn"`
	MaxConns        int32         `mapstructure:"max_conns"`
	MinConns        int32         `mapstructure:"min_conns"`
	MaxConnLifetime time.Duration `mapstructure:"max_conn_lifetime"`
	ConnectTimeout  time.Duration `mapstructure:"connect_timeout"`
}
