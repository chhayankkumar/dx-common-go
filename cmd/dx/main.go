// Command dx is the CDPG developer CLI: small, additive scaffolding helpers
// that keep every service's migration/sqlc layout identical without hiding
// or generating business logic. Run from a service repo's root.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		usage()
		os.Exit(2)
	}

	group, sub, rest := os.Args[1], os.Args[2], os.Args[3:]

	var err error
	switch {
	case group == "new" && sub == "migration":
		err = cmdNewMigration(rest)
	case group == "sqlc" && sub == "init":
		err = cmdSqlcInit(rest)
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "dx: "+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  dx new migration <name>   create a timestamped db/migrations/NNNN_<name>.up/.down.sql pair
  dx sqlc init               write a canonical sqlc.yaml + db/sqlc/{schema.sql,queries/}`)
}
