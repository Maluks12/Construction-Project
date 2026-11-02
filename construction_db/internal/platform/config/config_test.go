package config

import (
	"strings"
	"testing"

	"construction_db/internal/platform/storage"
)

func valid() Config {
	return Config{
		DatabaseURL:      "postgres://x",
		JWTSecret:        strings.Repeat("s", 32),
		S3:               storage.S3Config{Bucket: "evidence", Region: "af-south-1"},
		EvidenceLockMode: storage.LockCompliance,
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
