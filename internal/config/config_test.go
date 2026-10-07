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
	if cfg.Session.CookieName == "" {
		t.Error("expected default session cookie name to be non-empty")
	}
}
