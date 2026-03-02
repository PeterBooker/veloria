package main

import (
	"github.com/alecthomas/kong"
)

var cli struct {
	Serve       ServeCmd       `cmd:"" default:"withargs" help:"Start the HTTP server (default)."`
	Index       IndexCmd       `cmd:"" help:"Download, extract, and index a single extension."`
	Migrate     MigrateCmd     `cmd:"" help:"Run database migrations."`
	Wipe        WipeCmd        `cmd:"" help:"Wipe data from the database and storage."`
	Maintenance MaintenanceCmd `cmd:"" help:"Toggle maintenance mode on the running server."`
	Version     VersionCmd     `cmd:"" help:"Print version information."`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("veloria"),
		kong.Description("Code search engine for the WordPress ecosystem."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
