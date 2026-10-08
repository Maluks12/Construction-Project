package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL string
	Port        string
	JWTSecret   string
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
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
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

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variable: %s", strings.Join(missing, ", "))
	}

	if c.JWTSecret == ""{
		missing = append(missing, "JWT_SECRET")
	}

	if len(c.JWTSecret)< 32{
		return fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return nil
}
