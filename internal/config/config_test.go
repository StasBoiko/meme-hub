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
	if cfg.RabbitMQURL == "" {
		t.Error("expected default rabbitmq url to be non-empty")
	}
	if cfg.Session.CookieName == "" {
		t.Error("expected default session cookie name to be non-empty")
	}
	if cfg.SMTP.Host == "" {
		t.Error("expected default smtp host to be non-empty")
	}
	if cfg.SMTP.Port == "" {
		t.Error("expected default smtp port to be non-empty")
	}
	if cfg.SMTP.From == "" {
		t.Error("expected default smtp from address to be non-empty")
	}
	if cfg.SMTP.AdminEmail == "" {
		t.Error("expected default smtp admin email to be non-empty")
	}
	if cfg.ElasticsearchURL == "" {
		t.Error("expected default elasticsearch url to be non-empty")
	}
}

// TestLoadCustomEnv tests that custom environment variables override default values.
func TestLoadCustomEnv(t *testing.T) {
	tests := []struct {
		name                 string
		env                  map[string]string
		wantPort             string
		wantDBURL            string
		wantRedisURL         string
		wantRabbitMQURL      string
		wantClientID         string
		wantClientSecret     string
		wantRedirectURL      string
		wantSMTPHost         string
		wantSMTPPort         string
		wantSMTPUser         string
		wantSMTPPass         string
		wantSMTPFrom         string
		wantAdminEmail       string
		wantElasticsearchURL string
	}{
		{
			name: "custom environment variables",
			env: map[string]string{
				"PORT":                 "9090",
				"DATABASE_URL":         "postgres://user:pass@host:5432/db",
				"REDIS_URL":            "redis://custom-redis:6379/1",
				"RABBITMQ_URL":         "amqp://custom:secret@custom-rabbit:5672/",
				"GOOGLE_CLIENT_ID":     "my-client-id",
				"GOOGLE_CLIENT_SECRET": "my-client-secret",
				"GOOGLE_REDIRECT_URL":  "https://myapp.com/callback",
				"SMTP_HOST":            "smtp.custom.com",
				"SMTP_PORT":            "587",
				"SMTP_USER":            "custom_user",
				"SMTP_PASS":            "custom_pass",
				"SMTP_FROM":            "alerts@custom.com",
				"ADMIN_EMAIL":          "owner@custom.com",
				"SEARCH_URL":           "http://zinc.custom.com:4080",
				"SEARCH_USER":          "zincuser",
				"SEARCH_PASSWORD":      "zincpass",
			},
			wantPort:             "9090",
			wantDBURL:            "postgres://user:pass@host:5432/db",
			wantRedisURL:         "redis://custom-redis:6379/1",
			wantRabbitMQURL:      "amqp://custom:secret@custom-rabbit:5672/",
			wantClientID:         "my-client-id",
			wantClientSecret:     "my-client-secret",
			wantRedirectURL:      "https://myapp.com/callback",
			wantSMTPHost:         "smtp.custom.com",
			wantSMTPPort:         "587",
			wantSMTPUser:         "custom_user",
			wantSMTPPass:         "custom_pass",
			wantSMTPFrom:         "alerts@custom.com",
			wantAdminEmail:       "owner@custom.com",
			wantElasticsearchURL: "http://zinc.custom.com:4080",
		},
		{
			name: "custom port with default redirect url and smtp defaults",
			env: map[string]string{
				"PORT": "3000",
			},
			wantPort:             "3000",
			wantDBURL:            "postgres://postgres:secret@localhost:5433/meme_hub_db?sslmode=disable",
			wantRedisURL:         "redis://localhost:6380/0",
			wantRabbitMQURL:      "amqp://guest:guest@localhost:5672/",
			wantClientID:         "",
			wantClientSecret:     "",
			wantRedirectURL:      "http://localhost:3000/callback",
			wantSMTPHost:         "localhost",
			wantSMTPPort:         "1025",
			wantSMTPUser:         "",
			wantSMTPPass:         "",
			wantSMTPFrom:         "noreply@meme-hub.local",
			wantAdminEmail:       "admin@meme-hub.local",
			wantElasticsearchURL: "http://localhost:4080",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{
				"PORT", "DATABASE_URL", "REDIS_URL", "RABBITMQ_URL",
				"GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "GOOGLE_REDIRECT_URL",
				"SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM", "ADMIN_EMAIL",
				"ELASTICSEARCH_URL", "SEARCH_URL", "SEARCH_USER", "SEARCH_PASSWORD", "ZINC_URL", "ZINC_FIRST_ADMIN_USER", "ZINC_FIRST_ADMIN_PASSWORD",
			} {
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
			if cfg.RabbitMQURL != tc.wantRabbitMQURL {
				t.Errorf("cfg.RabbitMQURL = %q, want %q", cfg.RabbitMQURL, tc.wantRabbitMQURL)
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
			if cfg.SMTP.Host != tc.wantSMTPHost {
				t.Errorf("cfg.SMTP.Host = %q, want %q", cfg.SMTP.Host, tc.wantSMTPHost)
			}
			if cfg.SMTP.Port != tc.wantSMTPPort {
				t.Errorf("cfg.SMTP.Port = %q, want %q", cfg.SMTP.Port, tc.wantSMTPPort)
			}
			if cfg.SMTP.Username != tc.wantSMTPUser {
				t.Errorf("cfg.SMTP.Username = %q, want %q", cfg.SMTP.Username, tc.wantSMTPUser)
			}
			if cfg.SMTP.Password != tc.wantSMTPPass {
				t.Errorf("cfg.SMTP.Password = %q, want %q", cfg.SMTP.Password, tc.wantSMTPPass)
			}
			if cfg.SMTP.From != tc.wantSMTPFrom {
				t.Errorf("cfg.SMTP.From = %q, want %q", cfg.SMTP.From, tc.wantSMTPFrom)
			}
			if cfg.SMTP.AdminEmail != tc.wantAdminEmail {
				t.Errorf("cfg.SMTP.AdminEmail = %q, want %q", cfg.SMTP.AdminEmail, tc.wantAdminEmail)
			}
			if cfg.ElasticsearchURL != tc.wantElasticsearchURL {
				t.Errorf("cfg.ElasticsearchURL = %q, want %q", cfg.ElasticsearchURL, tc.wantElasticsearchURL)
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
