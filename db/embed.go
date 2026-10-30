// Package db holds the SQL migrations, embedded so the binary can migrate itself.
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
