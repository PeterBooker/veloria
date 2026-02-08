package auth

import (
	"strings"

	"github.com/markbates/goth"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/gitlab"

	"veloria/internal/client"
	"veloria/internal/config"
)

// Provider represents supported OAuth providers.
type Provider string

const (
	ProviderGitHub    Provider = "github"
	ProviderGitLab    Provider = "gitlab"
	ProviderAtlassian Provider = "atlassian"
)

// SetupProviders initializes OAuth providers based on configuration.
func SetupProviders(cfg *config.Config) {
	providers := []goth.Provider{}
	if cfg.OAuthBaseURL == "" {
		return
	}
	baseURL := strings.TrimRight(cfg.OAuthBaseURL, "/")

	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		p := github.New(
			cfg.GitHubClientID,
			cfg.GitHubClientSecret,
			baseURL+"/auth/github/callback",
			"user:email",
		)
		p.HTTPClient = client.GetAPI()
		providers = append(providers, p)
	}

	if cfg.GitLabClientID != "" && cfg.GitLabClientSecret != "" {
		p := gitlab.New(
			cfg.GitLabClientID,
			cfg.GitLabClientSecret,
			baseURL+"/auth/gitlab/callback",
			"read_user",
		)
		p.HTTPClient = client.GetAPI()
		providers = append(providers, p)
	}

	if cfg.AtlassianClientID != "" && cfg.AtlassianClientSecret != "" {
		p := NewAtlassianProvider(
			cfg.AtlassianClientID,
			cfg.AtlassianClientSecret,
			baseURL+"/auth/atlassian/callback",
		)
		p.HTTPClient = client.GetAPI()
		providers = append(providers, p)
	}

	if len(providers) > 0 {
		goth.UseProviders(providers...)
	}
}

// GetEnabledProviders returns a list of enabled provider names.
func GetEnabledProviders(cfg *config.Config) []string {
	var providers []string
	if cfg.OAuthBaseURL == "" {
		return providers
	}

	if cfg.GitHubClientID != "" && cfg.GitHubClientSecret != "" {
		providers = append(providers, string(ProviderGitHub))
	}
	if cfg.GitLabClientID != "" && cfg.GitLabClientSecret != "" {
		providers = append(providers, string(ProviderGitLab))
	}
	if cfg.AtlassianClientID != "" && cfg.AtlassianClientSecret != "" {
		providers = append(providers, string(ProviderAtlassian))
	}

	return providers
}
