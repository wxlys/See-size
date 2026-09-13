package main

import (
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/seesize/seesize/internal/hub"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "address for the hub HTTP server")
	token := flag.String("agent-token", os.Getenv("SEE_SIZE_AGENT_TOKEN"), "temporary shared Agent token")
	offlineAfter := flag.Duration("offline-after", 35*time.Second, "time without heartbeat before a server is offline")
	dataPath := flag.String("data", ".data/seesize.db", "SQLite database path")
	flag.Parse()

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

	server := &http.Server{
		Addr:              *listen,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("SeeSize hub listening", "address", *listen)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("hub stopped", "error", err)
		os.Exit(1)
	}
}
