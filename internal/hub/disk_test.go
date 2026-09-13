package hub

import (
	"bytes"
	"encoding/json"
	"github.com/seesize/seesize/internal/disk"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

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
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/servers/test/disk", nil))
	var result struct {
		Comparable bool          `json:"comparable"`
		Changes    []disk.Change `json:"changes"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Comparable || len(result.Changes) != 1 || result.Changes[0].Delta != 50 {
		t.Fatalf("bad comparison: %+v", result)
	}
}
