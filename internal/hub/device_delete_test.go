package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/seesize/seesize/internal/disk"
	"github.com/seesize/seesize/internal/model"
)

func TestDeleteDeviceIsolationAndReenrollment(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "delete.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	ctx := context.Background()
	h, cookie := acceptanceApp(t, store, now)
	call := func(method, path string, body any, c *http.Cookie, csrf bool) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		if c != nil {
			r.AddCookie(c)
		}
		if csrf {
			r.Header.Set("X-SeeSize-Request", "1")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, id := range []string{"delete-me", "keep-me"} {
		if _, err := store.db.Exec(`INSERT INTO devices VALUES(?,?,0)`, id, digest(id)); err != nil {
			t.Fatal(err)
		}
		if err := store.Upsert(ctx, model.Heartbeat{AgentID: id, CollectedAt: now}, "", now); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveDiskEvents(ctx, disk.Snapshot{AgentID: id, Root: "/test", At: now}, 10); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.Exec(`INSERT INTO disk_events(agent_id,root,path,delta,threshold,from_ns,to_ns) VALUES(?,'/test','.',10,10,1,2)`, id); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/devices/delete-me"
	body := map[string]string{"confirm_id": "delete-me"}
	if call("DELETE", path, body, nil, true).Code != 401 {
		t.Fatal("unauthorized deletion")
	}
	if call("DELETE", path, body, cookie, false).Code != 403 {
		t.Fatal("missing CSRF accepted")
	}
	if call("DELETE", path, map[string]string{"confirm_id": "wrong"}, cookie, true).Code != 400 {
		t.Fatal("bad confirmation accepted")
	}
	if call("DELETE", path, body, cookie, true).Code != 409 {
		t.Fatal("active credential deletion accepted")
	}
	if call("POST", path+"/revoke", nil, cookie, true).Code != 200 {
		t.Fatal("revoke failed")
	}
	if call("DELETE", path, body, cookie, true).Code != 200 {
		t.Fatal("delete failed")
	}
	if call("DELETE", path, body, cookie, true).Code != 404 {
		t.Fatal("repeat delete")
	}
	for _, table := range []string{"servers", "metric_samples", "disk_snapshots", "disk_events"} {
		var deleted, kept int
		if err := store.db.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE agent_id='delete-me'`).Scan(&deleted); err != nil {
			t.Fatal(err)
		}
		if err := store.db.QueryRow(`SELECT count(*) FROM ` + table + ` WHERE agent_id='keep-me'`).Scan(&kept); err != nil {
			t.Fatal(err)
		}
		if deleted != 0 || kept != 1 {
			t.Fatal(table, deleted, kept)
		}
	}
	// Simulates a previously authorized request reaching storage after deletion.
	if store.Upsert(ctx, model.Heartbeat{AgentID: "delete-me", CollectedAt: now}, "", now) == nil {
		t.Fatal("late heartbeat resurrected device")
	}
	if store.SaveDiskEvents(ctx, disk.Snapshot{AgentID: "delete-me", At: now}, 10) == nil {
		t.Fatal("late scan resurrected device")
	}
	var list struct {
		Devices []struct {
			ID string `json:"agent_id"`
		} `json:"devices"`
	}
	res := call("GET", "/api/v1/devices", nil, cookie, true)
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Devices) != 1 || list.Devices[0].ID != "keep-me" {
		t.Fatal("deleted device still listed")
	}
	res = call("POST", "/api/v1/enrollments", map[string]string{"agent_id": "delete-me"}, cookie, true)
	var code map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &code); err != nil {
		t.Fatal(err)
	}
	if call("POST", "/api/v1/agents/register", map[string]string{"agent_id": "delete-me", "code": code["code"]}, nil, false).Code != 201 {
		t.Fatal("explicit re-enrollment failed")
	}
	if err := store.Upsert(ctx, model.Heartbeat{AgentID: "delete-me", CollectedAt: now}, "", now); err != nil {
		t.Fatal(err)
	}
}
