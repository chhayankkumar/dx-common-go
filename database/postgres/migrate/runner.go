package migrate

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	"go.uber.org/zap"
)

// Run applies every pending migration under dir (an embedded directory of
// NNNN_title.up.sql / NNNN_title.down.sql pairs, see doc.go) to cfg.DSN,
// tracked in cfg.TableName. cfg.Mode == ModeNone is a no-op, so callers can
// invoke Run unconditionally from cmd/server/main.go and gate real
// execution purely through config. A nil logger is fine (logging is skipped).
func Run(cfg Config, fsys fs.FS, dir string, logger *zap.Logger) error {
	if cfg.Mode != ModeMigrate {
		return nil
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	m, closeFn, err := open(cfg, fsys, dir)
	if err != nil {
		return err
	}
	defer closeFn()

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			logger.Info("no pending migrations", zap.String("table", cfg.TableName))
			return nil
		}
		var dirty migrate.ErrDirty
		if errors.As(err, &dirty) {
			return &DirtyStateError{Version: uint(dirty.Version), Table: cfg.TableName}
		}
		return fmt.Errorf("migrate: up: %w", err)
	}

	version, _, verr := m.Version()
	if verr == nil {
		logger.Info("migrations applied", zap.String("table", cfg.TableName), zap.Uint("version", version))
	}
	return nil
}
