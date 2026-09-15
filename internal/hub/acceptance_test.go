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

// Exercise the authenticated HTTP handlers against a real, isolated SQLite DB.
func acceptanceApp(t *testing.T, store *SQLiteStore, now time.Time) (http.Handler, *http.Cookie) {
	t.Helper()
	app, err := NewServer("", store)
	if err != nil {
		t.Fatal(err)
	}
	app.now = func() time.Time { return now }
	if err := app.SetAdminToken("acceptance-admin-secret"); err != nil {
		t.Fatal(err)
	}
	if err := app.EnableAuthentication(); err != nil {
		t.Fatal(err)
	}
	h := app.Handler()
	r := httptest.NewRequest("POST", "/api/v1/login", bytes.NewBufferString(`{"credential":"acceptance-admin-secret"}`))
	r.Header.Set("X-SeeSize-Request", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	return h, w.Result().Cookies()[0]
}

func acceptanceGET(t *testing.T, h http.Handler, cookie *http.Cookie, path string, out any) {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatal(err)
	}
}

func TestAcceptanceFullDayTrendHTTP(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "day.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	start := now.Add(-24 * time.Hour)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, id := range []string{"day", "other"} {
		if _, err := tx.Exec(`INSERT INTO servers VALUES (?,'{}','',0)`, id); err != nil {
			t.Fatal(err)
		}
	}
	// 5-second cadence (>1000 records), a whole missing 4-minute bucket,
	// and alternating values make every range's expected aggregates exact.
	for at, i := start, 0; !at.After(now); at, i = at.Add(5*time.Second), i+1 {
		if !at.Before(now.Add(-8*time.Minute)) && at.Before(now.Add(-4*time.Minute)) {
			continue
		}
		v := 10
		if i%2 == 1 {
			v = 30
		}
		body, err := json.Marshal(model.Metrics{CPUPercent: float64(v), Memory: model.MemoryMetrics{UsedBytes: uint64(v), TotalBytes: 100}, RootDisk: model.DiskMetrics{UsedBytes: uint64(v), TotalBytes: 100}, Network: model.NetworkMetrics{ReceivedBytesPerSecond: float64(v), SentBytesPerSecond: float64(v)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO metric_samples VALUES (?,?,?)`, "day", at.UnixNano(), body); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		id string
		at time.Time
	}{{"day", start.Add(-time.Second)}, {"other", start}} {
		if _, err := tx.Exec(`INSERT INTO metric_samples VALUES (?,?,?)`, row.id, row.at.UnixNano(), `{"cpu_percent":99}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h, cookie := acceptanceApp(t, s, now)
	for _, tc := range []struct {
		label          string
		duration, step time.Duration
	}{{"30m", 30 * time.Minute, 5 * time.Second}, {"1h", time.Hour, 10 * time.Second}, {"24h", 24 * time.Hour, 240 * time.Second}} {
		t.Run(tc.label, func(t *testing.T) {
			var got Trend
			acceptanceGET(t, h, cookie, "/api/v1/servers/day/trend?range="+tc.label, &got)
			from := now.Add(-tc.duration)
			if !got.From.Equal(from) || !got.To.Equal(now) || got.StepSeconds != tc.step.Seconds() || got.First == nil || !got.First.Equal(from) || got.Last == nil || !got.Last.Equal(now.Add(-5*time.Second)) {
				t.Fatalf("range: %+v", got)
			}
			if len(got.Points) != 360-int(4*time.Minute/tc.step) {
				t.Fatalf("points: %d", len(got.Points))
			}
			count := 0
			for _, p := range got.Points {
				count += p.Count
				if !p.At.Before(now.Add(-8*time.Minute)) && p.At.Before(now.Add(-4*time.Minute)) {
					t.Fatal("gap filled")
				}
				if p.At.Sub(from)%tc.step != 0 || p.Count != int(tc.step/(5*time.Second)) {
					t.Fatalf("bucket: %+v", p)
				}
				avg, peak := 20.0, 30.0
				if tc.step == 5*time.Second {
					avg = 10
					if int(p.At.Sub(start)/(5*time.Second))%2 == 1 {
						avg = 30
					}
					peak = avg
				}
				for k := range p.Average {
					if p.Average[k] != avg || p.Peak[k] != peak {
						t.Fatalf("metric %d: %+v", k, p)
					}
				}
			}
			if count != int((tc.duration-4*time.Minute)/(5*time.Second)) {
				t.Fatalf("sample truncation: %d", count)
			}
		})
	}
	var empty Trend
	acceptanceGET(t, h, cookie, "/api/v1/servers/missing/trend?range=24h", &empty)
	if len(empty.Points) != 0 || empty.First != nil || empty.Last != nil {
		t.Fatal("empty history fabricated")
	}
}

func TestAcceptanceRetentionPreservesIdentityAndIngestion(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "retention.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)
	if _, err := s.db.Exec(`INSERT INTO devices VALUES (?,?,0)`, "test", digest("test-device-token")); err != nil {
		t.Fatal(err)
	}
	// Each increasing complete snapshot after the baseline produces an event.
	for i, at := range []time.Time{cutoff.Add(-3 * time.Second), cutoff.Add(-2 * time.Second), cutoff.Add(-time.Second), cutoff, now.Add(-time.Second)} {
		if err := s.Upsert(ctx, model.Heartbeat{AgentID: "test", Hostname: "test", CollectedAt: at}, "127.0.0.1", at); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveDiskEvents(ctx, disk.Snapshot{AgentID: "test", Root: "/isolated", At: at, Complete: true, Nodes: []disk.Node{{Path: ".", Bytes: int64(i * 100)}}}, 50); err != nil {
			t.Fatal(err)
		}
	}
	h, cookie := acceptanceApp(t, s, now)
	var before struct {
		Events []Event `json:"events"`
	}
	acceptanceGET(t, h, cookie, "/api/v1/servers/test/events", &before)
	if len(before.Events) != 4 {
		t.Fatalf("seed events: %d", len(before.Events))
	}
	total := CleanupResult{}
	for i := 0; i < 4; i++ {
		r, err := s.CleanupBatch(ctx, now, 24*time.Hour, 1)
		if err != nil {
			t.Fatal(err)
		}
		total.Metrics += r.Metrics
		total.Snapshots += r.Snapshots
		total.Events += r.Events
		// Query through the authenticated HTTP layer between cleanup batches.
		var trend Trend
		acceptanceGET(t, h, cookie, "/api/v1/servers/test/trend?range=24h", &trend)
		if len(trend.Points) != 2 {
			t.Fatalf("recent history lost: %+v", trend)
		}
	}
	if total.Metrics != 3 || total.Snapshots != 3 || total.Events != 2 {
		t.Fatalf("cleanup: %+v", total)
	}
	var after struct {
		Events []Event `json:"events"`
	}
	acceptanceGET(t, h, cookie, "/api/v1/servers/test/events", &after)
	if len(after.Events) != 2 {
		t.Fatalf("retained events: %+v", after)
	}
	for _, e := range after.Events {
		if e.To.Before(cutoff) {
			t.Fatal("expired event retained")
		}
	}
	snaps, err := s.DiskSnapshots(ctx, "test")
	if err != nil || len(snaps) != 2 {
		t.Fatalf("snapshots: %v %v", snaps, err)
	}
	for _, snap := range snaps {
		if snap.At.Before(cutoff) {
			t.Fatal("expired snapshot retained")
		}
	}
	var devices struct {
		Devices []struct {
			ID      string `json:"agent_id"`
			Revoked bool   `json:"revoked"`
		} `json:"devices"`
	}
	acceptanceGET(t, h, cookie, "/api/v1/devices", &devices)
	if len(devices.Devices) != 1 || devices.Devices[0].ID != "test" || devices.Devices[0].Revoked {
		t.Fatal("identity changed")
	}
	body, _ := json.Marshal(model.Heartbeat{ProtocolVersion: model.ProtocolVersion, AgentID: "test", Hostname: "test", CollectedAt: now})
	req := httptest.NewRequest("POST", "/api/v1/agents/heartbeat", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-device-token")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 202 {
		t.Fatalf("ingestion: %d %s", res.Code, res.Body.String())
	}
	history, err := s.History(ctx, "test", cutoff.Add(-time.Hour), now, 100)
	if err != nil || len(history) != 3 || !history[0].CollectedAt.Equal(cutoff) || !history[2].CollectedAt.Equal(now) {
		t.Fatalf("history: %+v %v", history, err)
	}
}
