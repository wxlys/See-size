package hub

import (
	"context"
	"encoding/json"
	"github.com/seesize/seesize/internal/disk"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestEventsPersistDeduplicateAcknowledgeExpire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db")
	store, err := OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ctx := context.Background()
	for _, i := range []int{0, 1, 1, 2} {
		snap := disk.Snapshot{AgentID: "test", Root: "/logs", At: now.Add(time.Duration(i) * time.Second), Complete: true, Nodes: []disk.Node{{Path: ".", Bytes: int64(100 + min(i, 1)*50)}}}
		if err := store.SaveDiskEvents(ctx, snap, 50); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	store, err = OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, _ := NewServer("agent-secret", store)
	if err := app.SetAdminToken("independent-admin-secret"); err != nil {
		t.Fatal(err)
	}
	list := func() []Event {
		t.Helper()
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/servers/test/events", nil))
		var data struct {
			Events []Event `json:"events"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data.Events
	}
	events := list()
	if len(events) != 1 || events[0].Delta != 50 {
		t.Fatalf("bad events %+v", events)
	}
	for _, token := range []string{"agent-secret", "independent-admin-secret", "independent-admin-secret"} {
		req := httptest.NewRequest("POST", "/api/v1/servers/test/events/1/ack", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		want := 200
		if token == "agent-secret" {
			want = 401
		}
		if rec.Code != want {
			t.Fatal(rec.Code, rec.Body.String())
		}
	}
	if list()[0].AcknowledgedAt == nil {
		t.Fatal("ack not saved")
	}
	cleanup, err := store.CleanupBatch(ctx, now.Add(48*time.Hour), 24*time.Hour, 100)
	if err != nil || cleanup.Events != 1 || len(list()) != 0 {
		t.Fatal("event expiry failed", cleanup, err)
	}
}
