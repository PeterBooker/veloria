package main

import (
	"context"
	"fmt"

	"veloria/internal/config"
	"veloria/migrations"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/pressly/goose/v3"
)

const fmtDBString = "host=%s user=%s password=%s dbname=%s port=%d sslmode=%s"

// MigrateCmd runs database migrations via goose.
type MigrateCmd struct {
	Command string   `arg:"" help:"Migration command (up, down, status, version, redo, reset, up-by-one, up-to, down-to, create, fix)."`
	Args    []string `arg:"" optional:"" help:"Additional arguments for the migration command."`
}

func (c *MigrateCmd) Run() error {
	// Always load .env from cwd so the migrate command works standalone on prod.
	_ = godotenv.Load(".env")

	cfg, err := config.New()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	dbString := fmt.Sprintf(fmtDBString, cfg.DBHost, cfg.DBUser, cfg.DBPass, cfg.DBName, cfg.DBPort, cfg.DBSSLMode)

	db, err := goose.OpenDBWithDriver("pgx", dbString)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations.FS)

	if err := goose.RunContext(context.Background(), c.Command, db, ".", c.Args...); err != nil {
		return fmt.Errorf("migrate %s: %w", c.Command, err)
	}
	return nil
}
