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

// MaintenanceCmd toggles maintenance mode on the running server.
type MaintenanceCmd struct {
	On  MaintenanceOnCmd  `cmd:"on" help:"Enable maintenance mode."`
	Off MaintenanceOffCmd `cmd:"off" help:"Disable maintenance mode."`
}

type MaintenanceOnCmd struct{}
type MaintenanceOffCmd struct{}

func (c *MaintenanceOnCmd) Run() error  { return sendMaintenance(true) }
func (c *MaintenanceOffCmd) Run() error { return sendMaintenance(false) }

// ctlRequest mirrors app.ctlRequest.
type ctlRequest struct {
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
}

// ctlResponse mirrors app.ctlResponse.
type ctlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func sendMaintenance(enabled bool) error {
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

	if err := json.NewEncoder(conn).Encode(ctlRequest{Action: "maintenance", Enabled: enabled}); err != nil {
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
