// The worker runs the reminder jobs (M11): every few seconds it tries to
// claim each due job in Redis; the instance that claims it runs it, recorded
// in job_runs. Correctness never depends on Redis: Postgres refuses a second
// copy of any reminder and any second send.
//
// It also builds audit packs (M12) in a loop of its own, so a long build
// never holds up the reminders. Postgres decides which instance builds
// which export (a claim with a lease; see internal/core/exports).
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"construction_db/internal/core/exports"
	"construction_db/internal/core/notifications"
	"construction_db/internal/editions/construction"
	"construction_db/internal/platform/cache"
	"construction_db/internal/platform/config"
	"construction_db/internal/platform/database"
	"construction_db/internal/platform/mail"
	"construction_db/internal/platform/storage"
	"construction_db/internal/regimes"
)

const tick = 10 * time.Second

// exportTick: how often the builder looks for queued exports.
const exportTick = 15 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.LoadWorker()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}
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
	if err := regimes.CheckDatabase(ctx, pool, reg); err != nil {
		slog.Error("check regimes", "error", err)
		os.Exit(1)
	}
	rc, err := cache.Connect(ctx, cfg.RedisURL)
	if err != nil {
		slog.Error("connect redis", "error", err)
		os.Exit(1)
	}
	defer rc.Close()

	var mailer notifications.Mailer = mail.Log{}
	// config.LoadWorker refuses smtp mode without SMTP_URL: a missing URL
	// never silently turns into log mode.
	if cfg.EmailMode == config.EmailSMTP {
		if mailer, err = mail.NewSMTP(cfg.SMTPURL); err != nil {
			slog.Error("smtp", "error", err)
			os.Exit(1)
		}
	}
	jobs := &notifications.Jobs{Pool: pool, Registry: reg, Mailer: mailer}
	host, _ := os.Hostname()
	instance := host + "-" + time.Now().UTC().Format("150405")
	// In this order: a holiday may move a deadline, a reminder queues an
	// email, and the emails go in the same tick.
	run := []struct {
		name string
		fn   func(context.Context) (int, error)
	}{
		{notifications.JobHolidays, jobs.Holidays},
		{notifications.JobReminders, func(ctx context.Context) (int, error) { return jobs.Reminders(ctx, time.Now()) }},
		{notifications.JobEmails, jobs.Emails},
	}
	evidenceStore, err := storage.NewS3(ctx, cfg.S3)
	if err != nil {
		slog.Error("connect evidence storage", "error", err)
		os.Exit(1)
	}
	exportCfg := cfg.S3
	exportCfg.Bucket = cfg.ExportBucket
	exportStore, err := storage.NewS3(ctx, exportCfg)
	if err != nil {
		slog.Error("connect export storage", "error", err)
		os.Exit(1)
	}
	builder := &exports.Builder{Pool: pool, Evidence: evidenceStore, Exports: exportStore,
		Contributor: construction.New().ExportsContributor(), Instance: instance}
	go func() {
		t := time.NewTicker(exportTick)
		defer t.Stop()
		for {
			if err := notifications.RecordRun(ctx, pool, notifications.JobExports, instance, builder.RunOnce); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("job failed", "job", notifications.JobExports, "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	slog.Info("worker starting", "instance", instance)

	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		if err := rc.Heartbeat(ctx, instance, 3*tick); err != nil {
			slog.Warn("heartbeat", "error", err)
		}
		for _, job := range run {
			name, fn := job.name, job.fn
			_, ok, err := rc.Claim(ctx, "job:"+name, notifications.Schedule[name])
			if err != nil {
				slog.Warn("claim", "job", name, "error", err)
				continue
			}
			if !ok {
				continue
			}
			if err := notifications.RecordRun(ctx, pool, name, instance, fn); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("job failed", "job", name, "error", err)
			}
		}
		if stale, err := notifications.Stale(ctx, pool, time.Now()); err == nil && len(stale) > 0 {
			slog.Error("jobs have not completed in twice their interval", "jobs", stale)
		}
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case <-t.C:
		}
	}
}
