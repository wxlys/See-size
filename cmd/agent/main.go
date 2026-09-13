package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/seesize/seesize/internal/collector"
)

const version = "0.1.0-dev"

func main() {
	hubURL := flag.String("hub", envOr("SEE_SIZE_HUB_URL", "http://127.0.0.1:8080"), "SeeSize hub base URL")
	token := flag.String("token", os.Getenv("SEE_SIZE_AGENT_TOKEN"), "Agent credential")
	agentID := flag.String("id", envOr("SEE_SIZE_AGENT_ID", defaultAgentID()), "stable Agent identifier")
	interval := flag.Duration("interval", 10*time.Second, "heartbeat interval")
	once := flag.Bool("once", false, "collect and send one heartbeat")
	flag.Parse()

	if strings.TrimSpace(*token) == "" || strings.TrimSpace(*agentID) == "" {
		slog.Error("agent token and id are required")
		os.Exit(2)
	}
	if *interval < time.Second {
		slog.Error("interval must be at least one second")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client := &http.Client{Timeout: 8 * time.Second}
	metricsCollector := collector.New()

	send := func() error {
		heartbeat, err := metricsCollector.Collect(*agentID, version)
		if err != nil {
			return err
		}
		body, err := json.Marshal(heartbeat)
		if err != nil {
			return err
		}
		endpoint := strings.TrimRight(*hubURL, "/") + "/api/v1/agents/heartbeat"
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Authorization", "Bearer "+*token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "seesize-agent/"+version)
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			return fmt.Errorf("hub returned %s", response.Status)
		}
		return nil
	}

	if err := send(); err != nil {
		slog.Error("heartbeat failed", "error", err)
		if *once {
			os.Exit(1)
		}
	} else {
		slog.Info("heartbeat accepted", "agent_id", *agentID)
	}
	if *once {
		return
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopped")
			return
		case <-ticker.C:
			if err := send(); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("heartbeat failed", "error", err)
			}
		}
	}
}

func defaultAgentID() string {
	if content, err := os.ReadFile("/etc/machine-id"); err == nil {
		if id := strings.TrimSpace(string(content)); id != "" {
			return id
		}
	}
	hostname, _ := os.Hostname()
	return hostname
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
