//go:build integration

package repo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"veloria/internal/repo"
	"veloria/internal/testutil"
	"veloria/migrations"
)

// TestNormalizedPluginFitsSchema proves against a real Postgres with the real
// migrations that the value which failed in production (SQLSTATE 22001) is
// rejected by the schema, and that the same record inserts cleanly once it
// has gone through UnmarshalJSON.
func TestNormalizedPluginFitsSchema(t *testing.T) {
	db := testutil.NewTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	goose.SetBaseFS(migrations.FS)
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(sqlDB, "."))

	// Shape observed on 2026-09-01: 3,584 zero-width characters then the header.
	upstreamName := strings.Repeat("\u200b\u200d", 1792) + "=== Dreamanual Toolkit"

	raw := repo.Plugin{
		Name:         upstreamName,
		Slug:         "dreamanual-toolkit",
		Version:      "1.4.0",
		Requires:     "6.4",
		Tested:       "7.1",
		RequiresPHP:  "7.4",
		DownloadLink: "https://example.com/dreamanual-toolkit.1.4.0.zip",
		Tags:         map[string]string{},
	}
	err = db.Create(&raw).Error
	require.Error(t, err)
	require.Contains(t, err.Error(), "SQLSTATE 22001")

	payload, err := json.Marshal(map[string]any{
		"name":          upstreamName,
		"slug":          "dreamanual-toolkit",
		"version":       "1.4.0",
		"requires":      "6.4",
		"tested":        "7.1",
		"requires_php":  "7.4",
		"download_link": "https://example.com/dreamanual-toolkit.1.4.0.zip",
	})
	require.NoError(t, err)

	var p repo.Plugin
	require.NoError(t, json.Unmarshal(payload, &p))
	require.NoError(t, db.Create(&p).Error)

	var stored repo.Plugin
	require.NoError(t, db.Where("slug = ?", "dreamanual-toolkit").First(&stored).Error)
	require.Equal(t, "Dreamanual Toolkit", stored.Name)
}
