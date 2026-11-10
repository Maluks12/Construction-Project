package config

import (
	"slices"
	"strings"
	"testing"
	"time"

	"construction_db/internal/platform/storage"
)

func valid() Config {
	return Config{
		DatabaseURL:      "postgres://x",
		JWTSecret:        strings.Repeat("s", 32),
		S3:               storage.S3Config{Bucket: "evidence", Region: "af-south-1"},
		EvidenceLockMode: storage.LockCompliance,
		RequestTimeout:   10 * time.Second,
		DBMaxConns:       20,
	}
}

// Load reads the environment; there is no .env in this package's directory.
func TestLoad(t *testing.T) {
	setRequired := func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
		t.Setenv("S3_BUCKET", "evidence")
		t.Setenv("EVIDENCE_LOCK_MODE", storage.LockGovernance)
		t.Setenv("S3_ACCESS_KEY_ID", "")
		t.Setenv("S3_SECRET_ACCESS_KEY", "")
		t.Setenv("TRUSTED_PROXIES", "")
	}

	t.Run("defaults", func(t *testing.T) {
		setRequired(t)
		t.Setenv("REQUEST_TIMEOUT", "")
		t.Setenv("DB_MAX_CONNS", "")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RequestTimeout != 10*time.Second || cfg.DBMaxConns != 20 {
			t.Errorf("request timeout %s, max conns %d; want 10s and 20", cfg.RequestTimeout, cfg.DBMaxConns)
		}
		if cfg.TrustedProxies != nil {
			t.Errorf("trusted proxies %q, want none", cfg.TrustedProxies)
		}
	})

	t.Run("trusted proxies", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TRUSTED_PROXIES", " 10.0.0.1, 172.16.0.0/12,,::1 ")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"10.0.0.1", "172.16.0.0/12", "::1"}; !slices.Equal(cfg.TrustedProxies, want) {
			t.Errorf("trusted proxies %q, want %q", cfg.TrustedProxies, want)
		}
	})

	t.Run("set", func(t *testing.T) {
		setRequired(t)
		t.Setenv("REQUEST_TIMEOUT", "1500ms")
		t.Setenv("DB_MAX_CONNS", "7")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RequestTimeout != 1500*time.Millisecond || cfg.DBMaxConns != 7 {
			t.Errorf("request timeout %s, max conns %d; want 1.5s and 7", cfg.RequestTimeout, cfg.DBMaxConns)
		}
	})

	bad := map[string][2]string{
		"timeout without a unit": {"REQUEST_TIMEOUT", "10"},
		"timeout not a duration": {"REQUEST_TIMEOUT", "soon"},
		"zero timeout":           {"REQUEST_TIMEOUT", "0s"},
		"max conns not a number": {"DB_MAX_CONNS", "many"},
		"max conns too large":    {"DB_MAX_CONNS", "3000000000"},
		"max conns zero":         {"DB_MAX_CONNS", "0"},
		"proxy not an address":   {"TRUSTED_PROXIES", "10.0.0.1,proxy.internal"},
		"proxy CIDR malformed":   {"TRUSTED_PROXIES", "10.0.0.0/33"},
	}
	for name, kv := range bad {
		t.Run(name, func(t *testing.T) {
			setRequired(t)
			t.Setenv(kv[0], kv[1])
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), kv[0]) {
				t.Errorf("%s=%s: got %v, want an error naming %s", kv[0], kv[1], err, kv[0])
			}
		})
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		change  func(*Config)
		wantErr string
	}{
		{"valid, default credential chain", func(*Config) {}, ""},
		{"valid, static credentials", func(c *Config) { c.S3.AccessKeyID, c.S3.SecretAccessKey = "id", "secret" }, ""},
		{"no bucket", func(c *Config) { c.S3.Bucket = "" }, "S3_BUCKET"},
		{"no lock mode", func(c *Config) { c.EvidenceLockMode = "" }, "EVIDENCE_LOCK_MODE"},
		{"unknown lock mode", func(c *Config) { c.EvidenceLockMode = "governance" }, "EVIDENCE_LOCK_MODE must be"},
		{"key without secret", func(c *Config) { c.S3.AccessKeyID = "id" }, "S3_SECRET_ACCESS_KEY"},
		{"secret without key", func(c *Config) { c.S3.SecretAccessKey = "secret" }, "S3_ACCESS_KEY_ID"},
		{"zero request timeout", func(c *Config) { c.RequestTimeout = 0 }, "REQUEST_TIMEOUT"},
		{"negative request timeout", func(c *Config) { c.RequestTimeout = -time.Second }, "REQUEST_TIMEOUT"},
		{"no pool connections", func(c *Config) { c.DBMaxConns = 0 }, "DB_MAX_CONNS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.change(&c)
			err := c.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("got %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got %v, want an error mentioning %q", err, tt.wantErr)
			}
		})
	}
}
