package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"veloria/internal/config"
	"veloria/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const (
	fmtDBString = "host=%s user=%s password=%s dbname=%s port=%d sslmode=disable"
)

var flags = flag.NewFlagSet("migrate", flag.ExitOnError)

func main() {
	flags.Usage = usage
	if err := flags.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	args := flags.Args()
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		flags.Usage()
		return
	}

	command := args[0]

	c, err := config.New()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	dbString := fmt.Sprintf(fmtDBString, c.DBHost, c.DBUser, c.DBPass, c.DBName, c.DBPort)

	db, err := goose.OpenDBWithDriver("pgx", dbString)
	if err != nil {
		log.Fatal(err.Error())
	}

	defer func() {
		if err := db.Close(); err != nil {
			log.Fatal(err.Error())
		}
	}()

	goose.SetBaseFS(migrations.FS)

	if err := goose.RunContext(context.Background(), command, db, ".", args[1:]...); err != nil {
		log.Fatalf("migrate %v: %v", command, err)
	}
}

func usage() {
	fmt.Println(usagePrefix)
	fmt.Println(usageCommands)
}

var (
	usagePrefix = `Usage: migrate COMMAND
Examples:
    migrate status
`

	usageCommands = `
Commands:
    up                   Migrate the DB to the most recent version available
    up-by-one            Migrate the DB up by 1
    up-to VERSION        Migrate the DB to a specific VERSION
    down                 Roll back the version by 1
    down-to VERSION      Roll back to a specific VERSION
    redo                 Re-run the latest migration
    reset                Roll back all migrations
    status               Dump the migration status for the current DB
    version              Print the current version of the database
    create NAME [sql|go] Creates new migration file with the current timestamp
    fix                  Apply sequential ordering to migrations`
)
