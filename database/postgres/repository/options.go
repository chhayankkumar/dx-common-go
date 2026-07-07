package repository

import "github.com/datakaveri/dx-common-go/database/postgres/dao"

// Option configures a Base at construction time.
//
// Note on generics: WithTable/WithID/WithDAOOption all need an explicit type
// argument at the call site — repository.WithTable[User]("user"), not
// repository.WithTable("user") — because Go cannot infer R across the
// nested New[User](pool, WithTable(...)) call (no bidirectional/
// expected-type inference for generic function arguments). This is a hard
// Go language constraint, not a design choice.
type Option[R any] func(*config[R])

type config[R any] struct {
	table   string
	idCol   string
	daoOpts []dao.Option[R]
}

// WithTable sets the table name. Omit it if R implements dao.TableDescriber
// — New falls back to that, exactly like dao.NewBaseDAOFromEntity. Requires
// an explicit type argument at the call site: WithTable[User](...).
func WithTable[R any](table string) Option[R] {
	return func(c *config[R]) { c.table = table }
}

// WithID sets the ID column (defaults to "id" if omitted, same as dao's
// default). Requires an explicit type argument at the call site:
// WithID[User](...).
func WithID[R any](column string) Option[R] {
	return func(c *config[R]) { c.idCol = column }
}

// WithDAOOption forwards any existing dao.Option (dao.WithSoftDeleteFilter,
// dao.WithAuditColumns, dao.WithSoftDeleteValues, ...) — repository.Option
// doesn't reinvent these, it just composes with what dao already has.
func WithDAOOption[R any](opt dao.Option[R]) Option[R] {
	return func(c *config[R]) { c.daoOpts = append(c.daoOpts, opt) }
}
