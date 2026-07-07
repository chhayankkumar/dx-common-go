// Package fixtures embeds the shared Postgres fixture schema used by the
// dao, repository, and sqlcx packages' real-Postgres integration tests
// (applied via dxtest/containers.WithSetupSQL). Lives here, not under any
// one package's testdata/, because Go doesn't let one package import
// another's testdata directory and three separate packages need this same
// schema.
package fixtures

import "embed"

//go:embed schema.sql
var FS embed.FS

// Dir is the directory (rooted at FS) WithSetupSQL should read from.
const Dir = "."
