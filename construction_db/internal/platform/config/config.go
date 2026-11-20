package config

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"construction_db/internal/platform/storage"
)

type Config struct {
	DatabaseURL string
	Port        string
	JWTSecret   string

	S3               storage.S3Config
	EvidenceLockMode string
	// The bucket audit packs go to (M12 D7): versioning on, no Object Lock, a
	// 7-day lifecycle rule. Same endpoint and credentials as S3.
	ExportBucket string

	RequestTimeout time.Duration // one deadline per request (httpapi.Deadline)
	DBMaxConns     int32         // pgxpool MaxConns

	// Proxies whose X-Forwarded-For is believed; empty means none, so the
	// client IP is the TCP peer. Gin trusts every proxy unless told otherwise.
	TrustedProxies []string
}

const (
	defaultRequestTimeout = 10 * time.Second
	defaultDBMaxConns     = 20
)

func Load() (*Config, error) {
	var err error = godotenv.Load()

	if err != nil {
		log.Println("Warning: . env file not found, using environment variables")
	}

	timeout := defaultRequestTimeout
	if s := os.Getenv("REQUEST_TIMEOUT"); s != "" {
		if timeout, err = time.ParseDuration(s); err != nil {
			return nil, fmt.Errorf("REQUEST_TIMEOUT must be a duration such as 10s: %w", err)
		}
	}
	maxConns := int64(defaultDBMaxConns)
	if s := os.Getenv("DB_MAX_CONNS"); s != "" {
		if maxConns, err = strconv.ParseInt(s, 10, 32); err != nil {
			return nil, fmt.Errorf("DB_MAX_CONNS must be a whole number: %w", err)
		}
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
		ExportBucket:     os.Getenv("S3_EXPORT_BUCKET"),
		RequestTimeout:   timeout,
		DBMaxConns:       int32(maxConns),
		TrustedProxies:   splitList(os.Getenv("TRUSTED_PROXIES")),
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

	if c.ExportBucket == "" {
		missing = append(missing, "S3_EXPORT_BUCKET")
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

	if c.RequestTimeout <= 0 {
		return fmt.Errorf("REQUEST_TIMEOUT must be more than zero")
	}

	if c.DBMaxConns < 1 {
		return fmt.Errorf("DB_MAX_CONNS must be at least 1")
	}

	for _, p := range c.TrustedProxies {
		if _, _, err := net.ParseCIDR(p); err != nil && net.ParseIP(p) == nil {
			return fmt.Errorf("TRUSTED_PROXIES: %q is not an IP address or CIDR", p)
		}
	}
	return nil
}

// splitList splits a comma-separated value, dropping blanks; nil if empty.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Worker is cmd/worker's configuration.
type Worker struct {
	DatabaseURL string
	DBMaxConns  int32
	RedisURL    string // required: the jobs' schedule
	SMTPURL     string // required unless EmailMode is "log"
	// "smtp" (the default: send through SMTP_URL) or "log" (only log each
	// email, recorded 'logged', never 'sent'). Never chosen by a missing URL.
	EmailMode string
	// The audit pack builder reads photos and files from S3.Bucket and writes
	// packs to ExportBucket (M12).
	S3           storage.S3Config
	ExportBucket string
}

const defaultWorkerMaxConns = 5

func LoadWorker() (*Worker, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}
	w := &Worker{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		DBMaxConns:  defaultWorkerMaxConns,
		RedisURL:    os.Getenv("REDIS_URL"),
		SMTPURL:     os.Getenv("SMTP_URL"),
		EmailMode:   os.Getenv("EMAIL_MODE"),
		S3: storage.S3Config{
			Bucket:          os.Getenv("S3_BUCKET"),
			Region:          os.Getenv("S3_REGION"),
			Endpoint:        os.Getenv("S3_ENDPOINT"),
			AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		},
		ExportBucket: os.Getenv("S3_EXPORT_BUCKET"),
	}
	if w.S3.Region == "" {
		w.S3.Region = "af-south-1"
	}
	if s := os.Getenv("WORKER_DB_MAX_CONNS"); s != "" {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("WORKER_DB_MAX_CONNS must be a whole number from 1")
		}
		w.DBMaxConns = int32(n)
	}
	var missing []string
	if w.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if w.RedisURL == "" {
		missing = append(missing, "REDIS_URL")
	}
	if w.S3.Bucket == "" {
		missing = append(missing, "S3_BUCKET")
	}
	if w.ExportBucket == "" {
		missing = append(missing, "S3_EXPORT_BUCKET")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required environment variable: %s", strings.Join(missing, ", "))
	}
	switch w.EmailMode {
	case "", EmailSMTP:
		w.EmailMode = EmailSMTP
		if w.SMTPURL == "" {
			return nil, fmt.Errorf("SMTP_URL is required; set EMAIL_MODE=log to only log emails (dev)")
		}
	case EmailLog:
		if w.SMTPURL != "" {
			return nil, fmt.Errorf("EMAIL_MODE=log with SMTP_URL set: choose one")
		}
	default:
		return nil, fmt.Errorf("EMAIL_MODE must be smtp or log")
	}
	return w, nil
}

// Email modes for cmd/worker (EMAIL_MODE).
const (
	EmailSMTP = "smtp"
	EmailLog  = "log"
)
