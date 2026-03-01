package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"veloria/internal/config"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

// WipeCmd groups destructive data-wiping subcommands.
type WipeCmd struct {
	DataSources WipeDataSourcesCmd `cmd:"data-sources" help:"Wipe all extension data (plugins, themes, cores) from the database."`
	Searches    WipeSearchesCmd    `cmd:"searches" help:"Wipe all searches from the database and S3 storage."`
}

// loadConfigForWipe loads .env and returns a validated config.
func loadConfigForWipe() (*config.Config, error) {
	_ = godotenv.Load(".env")
	cfg, err := config.New()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// openDB opens a raw *sql.DB connection via pgx.
func openDB(cfg *config.Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(fmtDBString, cfg.DBHost, cfg.DBUser, cfg.DBPass, cfg.DBName, cfg.DBPort, cfg.DBSSLMode)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	return db, nil
}

// confirmWipe prompts the user for confirmation. Returns true if confirmed.
func confirmWipe(message string) bool {
	fmt.Printf("%s Continue? [y/N] ", message)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
}
