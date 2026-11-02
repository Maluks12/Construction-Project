package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"construction_db/internal/app"
	"construction_db/internal/editions/construction"
	"construction_db/internal/platform/config"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/storage"
	"construction_db/internal/regimes"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	// Before connecting, so broken regime data fails fast even without a database.
	reg, err := regimes.Load()
	if err != nil {
		slog.Error("load regimes", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
	if err != nil {
		slog.Error("connect database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database pool", "max_conns", cfg.DBMaxConns, "request_timeout", cfg.RequestTimeout.String())

	if err := regimes.CheckDatabase(ctx, pool, reg); err != nil {
		slog.Error("check regimes", "error", err)
		os.Exit(1)
	}

	store, err := storage.NewS3(ctx, cfg.S3)
	if err != nil {
		slog.Error("connect evidence storage", "error", err)
		os.Exit(1)
	}
	if err := store.CheckObjectLock(ctx, cfg.EvidenceLockMode); err != nil {
		slog.Error("check evidence bucket", "error", err)
		os.Exit(1)
	}

	router, err := app.NewRouter(ctx, pool, cfg.JWTSecret, reg, construction.New(), store, cfg.RequestTimeout)
	if err != nil {
		slog.Error("build router", "error", err)
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown", "error", err)
	}

	slog.Info("server stopped")
}
