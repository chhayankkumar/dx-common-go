package main

import "embed"

// migrations holds this service's versioned schema, embedded so the binary
// is self-contained. dxmigrate.Run reads the "migrations" subdirectory.
//
//go:embed migrations/*.sql
var migrations embed.FS
