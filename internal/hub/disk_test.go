package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/seesize/seesize/internal/disk"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDiskPartialReferenceKeepsScopeAndDoesNotCompare(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "reference.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	base := disk.Snapshot{AgentID: "test", Root: "/logs", Depth: 3, At: now.Add(-time.Hour), Complete: true, Nodes: []disk.Node{{Path: ".", Bytes: 100}}}
	if err := store.SaveDisk(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	// A newer complete scan with a different display depth must not be the reference.
	other := base
	other.Depth = 2
	other.At = now.Add(-40 * time.Minute)
	if err := store.SaveDisk(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	other = base
	other.Root = "/other"
	other.At = now.Add(-30 * time.Minute)
	if err := store.SaveDisk(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	for _, age := range []time.Duration{20 * time.Minute, 10 * time.Minute} {
		partial := base
		partial.At = now.Add(-age)
		partial.Complete = false
		partial.Reason = "entry limit"
		partial.Nodes = []disk.Node{{Path: ".", Bytes: 10}}
		if err := store.SaveDisk(context.Background(), partial); err != nil {
			t.Fatal(err)
		}
	}
	server, _ := NewServer("secret", store)
	read := func() (*disk.Snapshot, bool) {
		t.Helper()
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/servers/test/disk", nil))
		if res.Code != 200 {
			t.Fatal(res.Body.String())
		}
		var body struct {
			Reference  *disk.Snapshot     `json:"reference_snapshot"`
			Comparable bool               `json:"comparable"`
			Changes    []disk.Change      `json:"changes"`
			Alerts     []disk.GrowthAlert `json:"alerts"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Changes) != 0 || len(body.Alerts) != 0 {
			t.Fatal("partial scan produced comparison or alerts")
		}
		return body.Reference, body.Comparable
	}
	ref, comparable := read()
	if ref == nil || !ref.At.Equal(base.At) || ref.Nodes[0].Bytes != 100 || comparable {
		t.Fatalf("wrong reference: %+v", ref)
	}
	// Existing retention still governs reference availability; nothing is pinned forever.
	if _, err := store.CleanupBatch(context.Background(), now, 45*time.Minute, 1000); err != nil {
		t.Fatal(err)
	}
	ref, comparable = read()
	if ref != nil || comparable {
		t.Fatal("expired or mismatched reference returned")
	}
}

func TestDiskUploadPersistenceAndComparison(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	store, err := OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := NewServer("secret", store)
	for i := 0; i < 2; i++ {
		snap := disk.Snapshot{AgentID: "test", Root: "/logs", At: time.Now().Add(time.Duration(i) * time.Second), Complete: true, Nodes: []disk.Node{{Path: ".", Bytes: int64(100 + i*50)}}}
		body, _ := json.Marshal(snap)
		req := httptest.NewRequest("POST", "/api/v1/agents/disk-snapshots", bytes.NewReader(body))
		res := httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		if res.Code != 401 {
			t.Fatal("unauthenticated upload accepted")
		}
		req = httptest.NewRequest("POST", "/api/v1/agents/disk-snapshots", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		res = httptest.NewRecorder()
		server.Handler().ServeHTTP(res, req)
		if res.Code != 202 {
			t.Fatal(res.Body.String())
		}
	}
	store.Close()
	store, err = OpenSQLite(path, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server, _ = NewServer("secret", store)
	if err := server.SetGrowthThreshold(50); err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/servers/test/disk", nil))
	var result struct {
		Comparable bool               `json:"comparable"`
		Changes    []disk.Change      `json:"changes"`
		Alerts     []disk.GrowthAlert `json:"alerts"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Comparable || len(result.Changes) != 1 || result.Changes[0].Delta != 50 {
		t.Fatalf("bad comparison: %+v", result)
	}
	if len(result.Alerts) != 1 || result.Alerts[0].Delta != 50 {
		t.Fatalf("bad growth alert: %+v", result.Alerts)
	}
}
