package hub

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seesize/seesize/internal/model"
)

func TestSQLiteStorePersistsServersAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seesize.db")
	now := time.Date(2026, 9, 13, 12, 0, 0, 123, time.UTC)
	store, err := OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat := model.Heartbeat{
		ProtocolVersion: model.ProtocolVersion,
		AgentID:         "server-1",
		Hostname:        "demo-server",
		CollectedAt:     now,
		Metrics:         model.Metrics{CPUPercent: 12.5},
	}
	if err := store.Upsert(context.Background(), heartbeat, "203.0.113.9", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	servers, err := store.List(context.Background(), now.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 || servers[0].ObservedIP != "203.0.113.9" || !servers[0].Online {
		t.Fatalf("unexpected persisted servers: %#v", servers)
	}
	samples, err := store.History(context.Background(), "server-1", now.Add(-time.Second), now.Add(time.Second), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 || samples[0].Metrics.CPUPercent != 12.5 || !samples[0].CollectedAt.Equal(now) {
		t.Fatalf("unexpected persisted samples: %#v", samples)
	}
}
