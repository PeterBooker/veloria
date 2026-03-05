package model

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

type Search struct {
	ID          uuid.UUID  `json:"id" gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	Status      string     `json:"status"`
	Private     bool       `json:"private"`
	Term        string     `json:"term"`
	Repo        string     `json:"repo"`
	Progress    uint8      `json:"progress" gorm:"-"`
	ResultsSize     *int64     `json:"results_size,omitempty" gorm:"column:results_size"`
	TotalMatches    *int       `json:"total_matches,omitempty" gorm:"column:total_matches"`
	TotalExtensions *int       `json:"total_extensions,omitempty" gorm:"column:total_extensions"`
	Results         any        `json:"results,omitempty" gorm:"-"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	DeletedAt   *time.Time `json:"deleted_at,omitempty"`
	UserID      *uuid.UUID `json:"user_id" gorm:"type:uuid"`
}

type Result struct {
	Slug           string        `json:"slug"`
	Name           string        `json:"name"`
	Version        string        `json:"version"`
	Homepage       string        `json:"homepage"`
	ActiveInstalls uint64        `json:"active_installs"`
	TotalMatches   uint64        `json:"total_matches"`
	Matches        []ResultMatch `json:"matches"`
}

type ResultMatch struct {
	Slug       string `json:"slug"`
	File       string `json:"file"`
	LineNumber int    `json:"line_number"`
	LineText   string `json:"line_text"`
}
