package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/seesize/seesize/internal/hub"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address for the hub HTTP server")
	token := flag.String("agent-token", os.Getenv("SEE_SIZE_AGENT_TOKEN"), "temporary shared Agent token")
	offlineAfter := flag.Duration("offline-after", 35*time.Second, "time without heartbeat before a server is offline")
	dataPath := flag.String("data", ".data/seesize.db", "SQLite database path")
	retentionDays := flag.Int("retention-days", 7, "days to retain metrics and disk snapshots (1..365)")
	growthMiB := flag.Int64("disk-growth-mib", 100, "directory growth warning threshold per scan interval (MiB)")
	cleanupInterval := flag.Duration("cleanup-interval", 10*time.Minute, "expired data cleanup interval (1s..24h)")
	flag.Parse()
	if *growthMiB < 1 || *growthMiB > 1048576 || *retentionDays < 1 || *retentionDays > 365 || *cleanupInterval < time.Second || *cleanupInterval > 24*time.Hour {
		slog.Error("retention days must be 1..365, cleanup interval 1s..24h, and disk growth threshold 1..1048576 MiB")
		os.Exit(2)
	}

	store, err := hub.OpenSQLite(*dataPath, *offlineAfter)
	if err != nil {
		slog.Error("unable to open hub database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	app, err := hub.NewServer(*token, store)
	if err != nil {
		slog.Error("hub configuration is invalid", "error", err)
		os.Exit(2)
	}
	if err := app.SetGrowthThreshold(*growthMiB << 20); err != nil {
		slog.Error("invalid growth threshold", "error", err)
		os.Exit(2)
	}
	if err := app.SetAdminToken(os.Getenv("SEE_SIZE_ADMIN_TOKEN")); err != nil {
		slog.Error("invalid management credential", "error", err)
		os.Exit(2)
	}

	if err := app.EnableAuthentication(); err != nil {
		slog.Error("login configuration invalid", "error", err)
		os.Exit(2)
	}
	server := &http.Server{
		Addr:              *listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		store.RunRetention(ctx, time.Duration(*retentionDays)*24*time.Hour, *cleanupInterval, func(result hub.CleanupResult, err error) {
			if err != nil && ctx.Err() == nil {
				slog.Warn("history cleanup incomplete", "error", err, "metrics_deleted", result.Metrics, "snapshots_deleted", result.Snapshots)
			} else if result.Metrics+result.Snapshots+result.Events > 0 {
				slog.Info("expired history removed", "metrics_deleted", result.Metrics, "snapshots_deleted", result.Snapshots, "events_deleted", result.Events)
			}
		})
	}()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Warn("HTTP shutdown", "error", err)
		}
	}()
	slog.Info("history retention enabled", "days", *retentionDays, "cleanup_interval", *cleanupInterval)
	slog.Info("SeeSize hub listening", "address", *listen)
	failed := false
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("hub stopped", "error", err)
		failed = true
	}
	stop()
	<-cleanupDone
	<-shutdownDone
	if failed {
		os.Exit(1)
	}
}
