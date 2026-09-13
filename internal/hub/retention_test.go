package hub

import (
	"context"
	"github.com/seesize/seesize/internal/disk"
	"github.com/seesize/seesize/internal/model"
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionBoundaryBatchAndInventory(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	for _, at := range []time.Time{cutoff.Add(-2 * time.Second), cutoff.Add(-time.Second), cutoff, now} {
		if err := store.Upsert(ctx, model.Heartbeat{AgentID: "a", Hostname: "a", CollectedAt: at}, "127.0.0.1", at); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDisk(ctx, disk.Snapshot{AgentID: "a", Root: "/logs", At: at}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		r, err := store.CleanupBatch(ctx, now, 24*time.Hour, 1)
		if err != nil {
			t.Fatal(err)
		}
		if r.Metrics != 1 || r.Snapshots != 1 {
			t.Fatalf("batch: %+v", r)
		}
	}
	r, err := store.CleanupBatch(ctx, now, 24*time.Hour, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Metrics+r.Snapshots != 0 {
		t.Fatal("deleted boundary or recent data")
	}
	history, err := store.History(ctx, "a", cutoff.Add(-time.Hour), now, 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("history %v %v", history, err)
	}
	snapshots, err := store.DiskSnapshots(ctx, "a")
	if err != nil || len(snapshots) != 2 {
		t.Fatalf("snapshots %v %v", snapshots, err)
	}
	servers, err := store.List(ctx, now)
	if err != nil || len(servers) != 1 {
		t.Fatal("inventory lost")
	}
	if _, err := store.CleanupBatch(ctx, now, 0, 1); err == nil {
		t.Fatal("invalid retention accepted")
	}
}
func TestRetentionStartupSweep(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Upsert(context.Background(), model.Heartbeat{AgentID: "old", CollectedAt: time.Now().Add(-48 * time.Hour)}, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		store.RunRetention(ctx, 24*time.Hour, time.Hour, func(r CleanupResult, err error) {
			if err != nil || r.Metrics != 1 {
				t.Errorf("startup cleanup: %+v %v", r, err)
			}
			cancel()
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not stop")
	}
}
