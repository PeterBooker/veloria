package config

import (
	"os"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func TestConfigDefaults(t *testing.T) {
	os.Clearenv()
	cfg, err := New()
	require.NoError(t, err)

	// Assert that default values are correctly set
	assert.Equal(t, "Veloria Core", cfg.Name)
	assert.Equal(t, 9071, cfg.Port)
	assert.Equal(t, "development", cfg.Env)
	assert.Equal(t, "1.0.0", cfg.Version)
	assert.Equal(t, "/etc/veloria/data", cfg.DataDir)
	assert.Equal(t, int64(2500), cfg.HTTPTimeout)
	assert.Equal(t, 30*time.Second, cfg.HTTPHandlerTimeout)
	assert.Equal(t, 30*time.Second, cfg.HTTPReadTimeout)
	assert.Equal(t, 5*time.Second, cfg.HTTPReadHeaderTimeout)
	assert.Equal(t, 30*time.Second, cfg.HTTPWriteTimeout)
	assert.Equal(t, 60*time.Second, cfg.HTTPIdleTimeout)
	assert.Equal(t, 10*time.Second, cfg.HTTPShutdownTimeout)
	assert.Equal(t, true, cfg.HTTPRateLimitEnabled)
	assert.Equal(t, true, cfg.HTTPLoggingEnabled)
	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, 5432, cfg.DBPort)
	assert.Equal(t, "fundy", cfg.DBName)
	assert.Equal(t, "root", cfg.DBUser)
	assert.Equal(t, "", cfg.DBPass)
	assert.Equal(t, "disable", cfg.DBSSLMode)
	assert.Equal(t, "UTC", cfg.DBTimeZone)
	assert.Equal(t, 5, cfg.DBConnectTimeout)
	assert.Equal(t, 3*time.Second, cfg.DBPingTimeout)
	assert.Equal(t, 10, cfg.DBMaxIdleConns)
	assert.Equal(t, 100, cfg.DBMaxOpenConns)
	assert.Equal(t, 10*time.Minute, cfg.DBConnMaxIdleTime)
	assert.Equal(t, time.Hour, cfg.DBConnMaxLifetime)
	assert.Equal(t, "", cfg.SentryDSN)
	assert.Equal(t, 0.0, cfg.SentrySampleRate)
	assert.Equal(t, 0.0, cfg.SentryTracesSampleRate)
	assert.Equal(t, true, cfg.S3EnsureBucket)
	assert.Equal(t, 5*time.Second, cfg.S3InitTimeout)
	assert.Equal(t, "", cfg.OAuthBaseURL)
	assert.Equal(t, "", cfg.SessionSecret)
	assert.Equal(t, "", cfg.AspireCloudAPIKey)
}

func TestConfigWithEnv(t *testing.T) {
	setEnv(t, "NAME", "fundy_blaze")
	setEnv(t, "PORT", "8080")
	setEnv(t, "ENV", "production")
	setEnv(t, "VERSION", "1.1.0")
	setEnv(t, "DATA_DIR", "/custom/data/dir")
	setEnv(t, "HTTP_TIMEOUT", "5000")
	setEnv(t, "HTTP_HANDLER_TIMEOUT", "12s")
	setEnv(t, "HTTP_READ_TIMEOUT", "11s")
	setEnv(t, "HTTP_READ_HEADER_TIMEOUT", "4s")
	setEnv(t, "HTTP_WRITE_TIMEOUT", "13s")
	setEnv(t, "HTTP_IDLE_TIMEOUT", "70s")
	setEnv(t, "HTTP_SHUTDOWN_TIMEOUT", "9s")
	setEnv(t, "HTTP_RATE_LIMIT_ENABLED", "false")
	setEnv(t, "HTTP_LOGGING_ENABLED", "false")
	setEnv(t, "DB_HOST", "customhost")
	setEnv(t, "DB_PORT", "5432")
	setEnv(t, "DB_DATABASE", "customdb")
	setEnv(t, "DB_USERNAME", "customuser")
	setEnv(t, "DB_PASSWORD", "custompass")
	setEnv(t, "DB_SSLMODE", "verify-full")
	setEnv(t, "DB_TIMEZONE", "Europe/London")
	setEnv(t, "DB_CONNECT_TIMEOUT", "7")
	setEnv(t, "DB_PING_TIMEOUT", "2s")
	setEnv(t, "DB_MAX_IDLE_CONNS", "3")
	setEnv(t, "DB_MAX_OPEN_CONNS", "9")
	setEnv(t, "DB_CONN_MAX_IDLE_TIME", "2m")
	setEnv(t, "DB_CONN_MAX_LIFETIME", "30m")
	setEnv(t, "SENTRY_DSN", "https://sentry.io/123456")
	setEnv(t, "SENTRY_SAMPLE_RATE", "1.0")
	setEnv(t, "SENTRY_TRACES_SAMPLE_RATE", "1.0")
	setEnv(t, "S3_ENSURE_BUCKET", "true")
	setEnv(t, "S3_INIT_TIMEOUT", "8s")
	setEnv(t, "OAUTH_BASE_URL", "https://example.com")
	setEnv(t, "SESSION_SECRET", "super-secret")
	setEnv(t, "ASPIRE_CLOUD_API_KEY", "test-aspire-key")
	cfg, err := New()
	require.NoError(t, err)

	// Assert that environment variables override the defaults
	assert.Equal(t, "fundy_blaze", cfg.Name)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, "1.1.0", cfg.Version)
	assert.Equal(t, "/custom/data/dir", cfg.DataDir)
	assert.Equal(t, int64(5000), cfg.HTTPTimeout)
	assert.Equal(t, 12*time.Second, cfg.HTTPHandlerTimeout)
	assert.Equal(t, 11*time.Second, cfg.HTTPReadTimeout)
	assert.Equal(t, 4*time.Second, cfg.HTTPReadHeaderTimeout)
	assert.Equal(t, 13*time.Second, cfg.HTTPWriteTimeout)
	assert.Equal(t, 70*time.Second, cfg.HTTPIdleTimeout)
	assert.Equal(t, 9*time.Second, cfg.HTTPShutdownTimeout)
	assert.Equal(t, false, cfg.HTTPRateLimitEnabled)
	assert.Equal(t, false, cfg.HTTPLoggingEnabled)
	assert.Equal(t, "customhost", cfg.DBHost)
	assert.Equal(t, 5432, cfg.DBPort)
	assert.Equal(t, "customdb", cfg.DBName)
	assert.Equal(t, "customuser", cfg.DBUser)
	assert.Equal(t, "custompass", cfg.DBPass)
	assert.Equal(t, "verify-full", cfg.DBSSLMode)
	assert.Equal(t, "Europe/London", cfg.DBTimeZone)
	assert.Equal(t, 7, cfg.DBConnectTimeout)
	assert.Equal(t, 2*time.Second, cfg.DBPingTimeout)
	assert.Equal(t, 3, cfg.DBMaxIdleConns)
	assert.Equal(t, 9, cfg.DBMaxOpenConns)
	assert.Equal(t, 2*time.Minute, cfg.DBConnMaxIdleTime)
	assert.Equal(t, 30*time.Minute, cfg.DBConnMaxLifetime)
	assert.Equal(t, "https://sentry.io/123456", cfg.SentryDSN)
	assert.Equal(t, 1.0, cfg.SentrySampleRate)
	assert.Equal(t, 1.0, cfg.SentryTracesSampleRate)
	assert.Equal(t, true, cfg.S3EnsureBucket)
	assert.Equal(t, 8*time.Second, cfg.S3InitTimeout)
	assert.Equal(t, "https://example.com", cfg.OAuthBaseURL)
	assert.Equal(t, "super-secret", cfg.SessionSecret)
	assert.Equal(t, "test-aspire-key", cfg.AspireCloudAPIKey)
}

func TestConfigValidation_InvalidPort(t *testing.T) {
	os.Clearenv()
	setEnv(t, "PORT", "0")
	_, err := New()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Port")
}

func TestConfigValidation_PortTooHigh(t *testing.T) {
	os.Clearenv()
	setEnv(t, "PORT", "99999")
	_, err := New()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Port")
}

func TestConfigValidation_InvalidDBPort(t *testing.T) {
	os.Clearenv()
	setEnv(t, "DB_PORT", "0")
	_, err := New()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DBPort")
}

func TestConfigValidation_InvalidConcurrency(t *testing.T) {
	os.Clearenv()
	setEnv(t, "INDEXER_CONCURRENCY", "0")
	_, err := New()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "IndexerConcurrency")
}

func TestConfigValidation_RequiredFields(t *testing.T) {
	v := validator.New()
	cfg := &Config{
		Port:               9071,
		DBPort:             5432,
		IndexerConcurrency: 1,
		// DBHost, DBName, DBUser, DataDir left empty — should fail required
	}
	err := v.Struct(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DataDir")
	assert.Contains(t, err.Error(), "DBHost")
	assert.Contains(t, err.Error(), "DBName")
	assert.Contains(t, err.Error(), "DBUser")
}
