package migrate

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
)

// Status reports the current schema version and dirty flag without applying
// anything, for a boot-time "refuse to start if the DB is ahead of this
// binary" check. version=0, dirty=false, err=nil means no migration has run yet.
func Status(cfg Config, fsys fs.FS, dir string) (version uint, dirty bool, err error) {
	m, closeFn, err := open(cfg, fsys, dir)
	if err != nil {
		return 0, false, err
	}
	defer closeFn()

	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("migrate: version: %w", err)
	}
	return version, dirty, nil
}
