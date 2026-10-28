// Package config stores application configuration settings and environment variable loaders.
package config

import (
	"testing"
)

// TestConfigAddr tests that Config.Addr returns the correctly formatted listen address.
func TestConfigAddr(t *testing.T) {
	tests := []struct {
		name string
		port string
		want string
	}{
		{
			name: "plain port number",
			port: "8080",
			want: ":8080",
		},
		{
			name: "port with leading colon",
			port: ":9090",
			want: ":9090",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Port: tc.port}
			if got := cfg.Addr(); got != tc.want {
				t.Fatalf("cfg.Addr() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLoad tests that default configuration values are properly populated.
func TestLoad(t *testing.T) {
	cfg := Load()
	if cfg.Port == "" {
		t.Error("expected default port to be non-empty")
	}
	if cfg.DatabaseURL == "" {
		t.Error("expected default database url to be non-empty")
	}
	if cfg.RedisURL == "" {
		t.Error("expected default redis url to be non-empty")
	}
	if cfg.Session.CookieName == "" {
		t.Error("expected default session cookie name to be non-empty")
	}
}

// TestLoadCustomEnv tests that custom environment variables override default values.
func TestLoadCustomEnv(t *testing.T) {
	tests := []struct {
		name             string
		env              map[string]string
		wantPort         string
		wantDBURL        string
		wantRedisURL     string
		wantClientID     string
		wantClientSecret string
		wantRedirectURL  string
	}{
		{
			name: "custom environment variables",
			env: map[string]string{
				"PORT":                 "9090",
				"DATABASE_URL":         "postgres://user:pass@host:5432/db",
				"REDIS_URL":            "redis://custom-redis:6379/1",
				"GOOGLE_CLIENT_ID":     "my-client-id",
				"GOOGLE_CLIENT_SECRET": "my-client-secret",
				"GOOGLE_REDIRECT_URL":  "https://myapp.com/callback",
			},
			wantPort:         "9090",
			wantDBURL:        "postgres://user:pass@host:5432/db",
			wantRedisURL:     "redis://custom-redis:6379/1",
			wantClientID:     "my-client-id",
			wantClientSecret: "my-client-secret",
			wantRedirectURL:  "https://myapp.com/callback",
		},
		{
			name: "custom port with default redirect url",
			env: map[string]string{
				"PORT": "3000",
			},
			wantPort:         "3000",
			wantDBURL:        "postgres://postgres:secret@localhost:5433/guestbook_db?sslmode=disable",
			wantRedisURL:     "redis://localhost:6379/0",
			wantClientID:     "",
			wantClientSecret: "",
			wantRedirectURL:  "http://localhost:3000/callback",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"PORT", "DATABASE_URL", "REDIS_URL", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REDIRECT_URL"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg := Load()
			if cfg.Port != tc.wantPort {
				t.Errorf("cfg.Port = %q, want %q", cfg.Port, tc.wantPort)
			}
			if cfg.DatabaseURL != tc.wantDBURL {
				t.Errorf("cfg.DatabaseURL = %q, want %q", cfg.DatabaseURL, tc.wantDBURL)
			}
			if cfg.RedisURL != tc.wantRedisURL {
				t.Errorf("cfg.RedisURL = %q, want %q", cfg.RedisURL, tc.wantRedisURL)
			}
			if cfg.OAuth.ClientID != tc.wantClientID {
				t.Errorf("cfg.OAuth.ClientID = %q, want %q", cfg.OAuth.ClientID, tc.wantClientID)
			}
			if cfg.OAuth.ClientSecret != tc.wantClientSecret {
				t.Errorf("cfg.OAuth.ClientSecret = %q, want %q", cfg.OAuth.ClientSecret, tc.wantClientSecret)
			}
			if cfg.OAuth.RedirectURL != tc.wantRedirectURL {
				t.Errorf("cfg.OAuth.RedirectURL = %q, want %q", cfg.OAuth.RedirectURL, tc.wantRedirectURL)
			}
		})
	}
}

// TestLoadConfig tests that LoadConfig alias returns identical configuration to Load.
func TestLoadConfig(t *testing.T) {
	c1 := Load()
	c2 := LoadConfig()
	if c1 != c2 {
		t.Errorf("expected LoadConfig() to equal Load(), got %v vs %v", c2, c1)
	}
}
