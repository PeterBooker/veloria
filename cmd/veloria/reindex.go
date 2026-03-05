package main

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"veloria/internal/config"

	"github.com/joho/godotenv"
)

// ReindexCmd queues a re-index for an extension via the running server.
type ReindexCmd struct {
	Repo string `arg:"" help:"Repository type (plugins, themes, or cores)."`
	Slug string `arg:"" help:"Extension slug (or version number for cores)."`
}

func (c *ReindexCmd) Run() error {
	_ = godotenv.Load(".env")
	cfg, err := config.New()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	sockPath := filepath.Join(cfg.DataDir, "veloria.sock")
	conn, err := net.DialTimeout("unix", sockPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to control socket at %s: %w\nIs the server running?", sockPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	req := ctlRequest{
		Action:   "reindex",
		RepoType: c.Repo,
		Slug:     c.Slug,
	}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	var resp ctlResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.OK {
		return fmt.Errorf("server error: %s", resp.Message)
	}

	fmt.Println(resp.Message)
	return nil
}
