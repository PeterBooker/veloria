package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v10"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
)

type Config struct {
	Name                   string        `env:"NAME" envDefault:"Veloria Core"`
	Port                   int           `env:"PORT" envDefault:"9071" validate:"min=1,max=65535"`
	Env                    string        `env:"ENV" envDefault:"development"`
	Version                string        `env:"VERSION" envDefault:"1.0.0"`
	WorkingDir             string        `envDefault:"/"`
	DataDir                string        `env:"DATA_DIR" envDefault:"/etc/veloria/data" validate:"required"`
	HTTPTimeout            int64         `env:"HTTP_TIMEOUT" envDefault:"2500"`
	HTTPHandlerTimeout     time.Duration `env:"HTTP_HANDLER_TIMEOUT" envDefault:"30s"`
	HTTPReadTimeout        time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"30s"`
	HTTPReadHeaderTimeout  time.Duration `env:"HTTP_READ_HEADER_TIMEOUT" envDefault:"5s"`
	HTTPWriteTimeout       time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	HTTPIdleTimeout        time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"60s"`
	HTTPShutdownTimeout    time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	HTTPRateLimitEnabled   bool          `env:"HTTP_RATE_LIMIT_ENABLED" envDefault:"true"`
	HTTPLoggingEnabled     bool          `env:"HTTP_LOGGING_ENABLED" envDefault:"true"`
	DBHost                 string        `env:"DB_HOST" envDefault:"localhost" validate:"required"`
	DBPort                 int           `env:"DB_PORT" envDefault:"5432" validate:"min=1,max=65535"`
	DBName                 string        `env:"DB_DATABASE" envDefault:"fundy" validate:"required"`
	DBUser                 string        `env:"DB_USERNAME" envDefault:"root" validate:"required"`
	DBPass                 string        `env:"DB_PASSWORD" envDefault:""`
	DBSSLMode              string        `env:"DB_SSLMODE" envDefault:""`
	DBTimeZone             string        `env:"DB_TIMEZONE" envDefault:""`
	DBConnectTimeout       int           `env:"DB_CONNECT_TIMEOUT" envDefault:"5"`
	DBPingTimeout          time.Duration `env:"DB_PING_TIMEOUT" envDefault:"3s"`
	DBMaxIdleConns         int           `env:"DB_MAX_IDLE_CONNS" envDefault:"10"`
	DBMaxOpenConns         int           `env:"DB_MAX_OPEN_CONNS" envDefault:"100"`
	DBConnMaxIdleTime      time.Duration `env:"DB_CONN_MAX_IDLE_TIME" envDefault:"10m"`
	DBConnMaxLifetime      time.Duration `env:"DB_CONN_MAX_LIFETIME" envDefault:"1h"`
	SentryDSN              string        `env:"SENTRY_DSN" envDefault:""`
	SentrySampleRate       float64       `env:"SENTRY_SAMPLE_RATE" envDefault:"0.0"`
	SentryTracesSampleRate float64       `env:"SENTRY_TRACES_SAMPLE_RATE" envDefault:"0.0"`

	// S3/MinIO Configuration
	S3Endpoint     string        `env:"S3_ENDPOINT" envDefault:"localhost:9000"`
	S3Bucket       string        `env:"S3_BUCKET" envDefault:"veloria-searches"`
	S3AccessKey    string        `env:"S3_ACCESS_KEY" envDefault:"minioadmin"`
	S3SecretKey    string        `env:"S3_SECRET_KEY" envDefault:"minioadmin"`
	S3UseSSL       bool          `env:"S3_USE_SSL" envDefault:"false"`
	S3Region       string        `env:"S3_REGION" envDefault:"us-east-1"`
	S3EnsureBucket bool          `env:"S3_ENSURE_BUCKET" envDefault:"false"`
	S3InitTimeout  time.Duration `env:"S3_INIT_TIMEOUT" envDefault:"5s"`

	// OAuth Configuration
	OAuthBaseURL          string `env:"OAUTH_BASE_URL" envDefault:""`
	GitHubClientID        string `env:"GITHUB_CLIENT_ID" envDefault:""`
	GitHubClientSecret    string `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	GitLabClientID        string `env:"GITLAB_CLIENT_ID" envDefault:""`
	GitLabClientSecret    string `env:"GITLAB_CLIENT_SECRET" envDefault:""`
	AtlassianClientID     string `env:"ATLASSIAN_CLIENT_ID" envDefault:""`
	AtlassianClientSecret string `env:"ATLASSIAN_CLIENT_SECRET" envDefault:""`
	SessionSecret         string `env:"SESSION_SECRET" envDefault:""`

	// Indexer concurrency: number of parallel goroutines for indexing
	IndexerConcurrency int `env:"INDEXER_CONCURRENCY" envDefault:"1" validate:"min=1"`

	// Search concurrency: max concurrent search requests
	SearchConcurrency int `env:"SEARCH_CONCURRENCY" envDefault:"6" validate:"min=1"`

	// AspireCloud API
	AspireCloudAPIKey string `env:"ASPIRE_CLOUD_API_KEY" envDefault:""`

	// Debug mode: controls informational (non-error/warning) log output
	AppDebug bool `env:"APP_DEBUG" envDefault:"false"`

	// Application URL (production TLS via certmagic)
	AppURL             string `env:"APP_URL" envDefault:""`
	ACMEEmail          string `env:"ACME_EMAIL" envDefault:""`
	CloudflareAPIToken string `env:"CLOUDFLARE_API_TOKEN" envDefault:""`
}

// EnsureDirs creates all required data directories if they don't already exist.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		filepath.Join(c.DataDir, "plugins", "source"),
		filepath.Join(c.DataDir, "plugins", "index"),
		filepath.Join(c.DataDir, "themes", "source"),
		filepath.Join(c.DataDir, "themes", "index"),
		filepath.Join(c.DataDir, "cores", "source"),
		filepath.Join(c.DataDir, "cores", "index"),
		filepath.Join(c.DataDir, "certs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %q: %w", dir, err)
		}
	}
	return nil
}

func New() (*Config, error) {
	if envValue := os.Getenv("ENV"); envValue == "" || envValue == "development" {
		_ = godotenv.Load(".env")
	}

	c := &Config{}
	if err := env.Parse(c); err != nil {
		return nil, fmt.Errorf("failed to parse environment: %w", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	c.WorkingDir = wd

	sslMode := strings.TrimSpace(strings.ToLower(c.DBSSLMode))
	switch sslMode {
	case "", "false", "0", "disable":
		if sslMode == "false" || sslMode == "0" {
			c.DBSSLMode = "disable"
		} else if c.DBSSLMode == "" {
			if c.Env == "development" {
				c.DBSSLMode = "disable"
			} else {
				c.DBSSLMode = "require"
			}
		} else {
			c.DBSSLMode = "disable"
		}
	case "true", "1":
		c.DBSSLMode = "require"
	case "require", "verify-full", "verify-ca", "prefer", "allow":
		c.DBSSLMode = sslMode
	default:
		// Fall back to environment-appropriate default
		if c.Env == "development" {
			c.DBSSLMode = "disable"
		} else {
			c.DBSSLMode = "require"
		}
	}
	if c.DBTimeZone == "" {
		c.DBTimeZone = "UTC"
	}
	if _, ok := os.LookupEnv("S3_ENSURE_BUCKET"); !ok && c.Env == "development" {
		c.S3EnsureBucket = true
	}

	if err := validator.New().Struct(c); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	if c.Env != "development" && c.AppURL != "" && c.CloudflareAPIToken == "" {
		return nil, fmt.Errorf("invalid configuration: CLOUDFLARE_API_TOKEN is required when APP_URL is set in non-development mode")
	}

	return c, nil
}
