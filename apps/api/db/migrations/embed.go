// Package migrations embeds the goose SQL migrations so the api binary can run them without the files on disk.
package migrations

import "embed"

// FS holds every *.sql migration in this directory (goose naming: 00001_init.sql, ...).
//
//go:embed *.sql
var FS embed.FS
