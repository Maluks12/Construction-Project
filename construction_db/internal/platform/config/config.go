package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"construction_db/internal/platform/storage"
)

type Config struct {
	DatabaseURL string
	Port        string
	JWTSecret   string

	S3               storage.S3Config
	EvidenceLockMode string
}

func Load() (*Config, error) {
	var err error = godotenv.Load()

	if err != nil {
		log.Println("Warning: . env file not found, using environment variables")
	}

	cfg := &Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        os.Getenv("PORT"),
		JWTSecret:   os.Getenv("JWT_SECRET"),
		S3: storage.S3Config{
			Bucket:          os.Getenv("S3_BUCKET"),
			Region:          os.Getenv("S3_REGION"),
			Endpoint:        os.Getenv("S3_ENDPOINT"),
			PublicEndpoint:  os.Getenv("S3_PUBLIC_ENDPOINT"),
			AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		},
		EvidenceLockMode: os.Getenv("EVIDENCE_LOCK_MODE"),
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.S3.Region == "" {
		cfg.S3.Region = "af-south-1"
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}

	if c.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}

	if c.S3.Bucket == "" {
		missing = append(missing, "S3_BUCKET")
	}

	if c.EvidenceLockMode == "" {
		missing = append(missing, "EVIDENCE_LOCK_MODE")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variable: %s", strings.Join(missing, ", "))
	}

	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}

	if !storage.ValidLockMode(c.EvidenceLockMode) {
		return fmt.Errorf("EVIDENCE_LOCK_MODE must be %s or %s", storage.LockGovernance, storage.LockCompliance)
	}

	if (c.S3.AccessKeyID == "") != (c.S3.SecretAccessKey == "") {
		return fmt.Errorf("set both S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY, or neither")
	}
	return nil
}
