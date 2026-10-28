// Package config stores application configuration settings and environment variable loaders.
package config

import (
	"os"
	"strings"
	"time"
)

// Config stores application configuration settings.
type Config struct {
	// Port specifies the HTTP server port to listen on.
	Port string
	// DatabaseURL contains the PostgreSQL connection string.
	DatabaseURL string
	// RedisURL contains the Redis connection string.
	RedisURL string
	// OAuth contains Google OAuth2 configuration parameters.
	OAuth OAuthConfig
	// Session contains user session configuration parameters.
	Session SessionConfig
}

// OAuthConfig stores parameters for Google OAuth2 authentication.
type OAuthConfig struct {
	// ClientID is the Google OAuth2 application client identifier.
	ClientID string
	// ClientSecret is the Google OAuth2 client secret.
	ClientSecret string
	// RedirectURL is the OAuth2 callback redirect URL.
	RedirectURL string
}

// SessionConfig stores configuration parameters for user sessions.
type SessionConfig struct {
	// CookieName is the name of the session cookie.
	CookieName string
	// Duration defines the validity duration of user sessions.
	Duration time.Duration
}

// Addr returns the network listen address in the format ":port".
func (c Config) Addr() string {
	if strings.HasPrefix(c.Port, ":") {
		return c.Port
	}
	return ":" + c.Port
}

// Load reads configuration from environment variables or applies defaults.
func Load() Config {
	port := getEnv("PORT", "8080")
	redirectURL := getEnv("GOOGLE_REDIRECT_URL", "http://localhost:"+port+"/callback")

	return Config{
		Port:        port,
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5433/guestbook_db?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6379/0"),
		OAuth: OAuthConfig{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  redirectURL,
		},
		Session: SessionConfig{
			CookieName: "session",
			Duration:   7 * 24 * time.Hour,
		},
	}
}

// LoadConfig reads configuration from environment variables or applies defaults.
func LoadConfig() Config {
	return Load()
}

// getEnv retrieves an environment variable or returns the default value if unset.
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
