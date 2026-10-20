package hub

import (
	"context"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestTrendHistoricalWindowBounds(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "windows.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, _ := NewServer("secret", store)
	now := time.Date(2026, 10, 20, 12, 15, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	for _, tc := range []struct {
		span, end string
		status    int
	}{
		{"30m", "2026-10-20T12:00:00Z", 200},
		{"1h", "2026-10-20T02:00:00Z", 200},
		{"30m", "2026-10-20T06:30:00Z", 400},
		{"1h", "2026-10-20T01:00:00Z", 400},
		{"30m", "2026-10-20T13:00:00Z", 400},
		{"24h", "2026-10-20T12:00:00Z", 400},
		{"1h", "invalid", 400},
	} {
		res := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/servers/test/trend?range="+tc.span+"&to="+url.QueryEscape(tc.end), nil)
		app.Handler().ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, res.Code, res.Body.String())
		}
	}
}

func TestTrendAllRecordsGapsAndPeaks(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "trend.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	from := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`INSERT INTO servers VALUES ('test','{}','',0)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1200; i++ {
		body := `{"cpu_percent":10}`
		if i == 0 {
			body = `{"cpu_percent":100}`
		}
		_, err = tx.Exec(`INSERT INTO metric_samples VALUES (?,?,?)`, "test", from.Add(time.Duration(i)*time.Second).UnixNano(), body)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = tx.Exec(`INSERT INTO metric_samples VALUES (?,?,?)`, "test", from.Add(50*time.Minute).UnixNano(), `{"cpu_percent":20}`)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result, err := s.Trend(context.Background(), "test", from, to)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range result.Points {
		count += p.Count
	}
	if count != 1201 || len(result.Points) != 121 || result.Points[0].Average[0] != 19 || result.Points[0].Peak[0] != 100 {
		t.Fatalf("bad aggregation: count=%d first=%+v points=%d", count, result.Points[0], len(result.Points))
	}
	if !result.First.Equal(from) || result.StepSeconds != 10 {
		t.Fatal("range mismatch")
	}
	app, _ := NewServer("secret", s)
	res := httptest.NewRecorder()
	app.Handler().ServeHTTP(res, httptest.NewRequest("GET", "/api/v1/servers/test/trend?range=bad", nil))
	if res.Code != 400 {
		t.Fatal("invalid range accepted")
	}
}

func TestTrendNetworkUnavailableAndScopeChanges(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "network.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	from := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	if _, err = s.db.Exec(`INSERT INTO servers VALUES ('test','{}','',0)`); err != nil {
		t.Fatal(err)
	}
	bodies := []string{
		`{"cpu_percent":10,"network":{"received_bytes_per_second":12}}`,
		`{"cpu_percent":10,"network":{"scope":"unavailable"}}`,
		`{"cpu_percent":10,"network":{"scope":"selected","interfaces":["eth0"],"rate_unavailable":true}}`,
		`{"cpu_percent":10,"network":{"scope":"selected","interfaces":["eth0"],"received_bytes_per_second":20}}`,
		`{"cpu_percent":10,"network":{"scope":"selected","interfaces":["eth1"],"received_bytes_per_second":30}}`,
	}
	for i, body := range bodies {
		offset := time.Duration(i) * 10 * time.Second
		if i == 4 {
			offset = 31 * time.Second
		}
		if _, err = s.db.Exec(`INSERT INTO metric_samples VALUES (?,?,?)`, "test", from.Add(offset).UnixNano(), body); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Trend(context.Background(), "test", from, from.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 4 {
		t.Fatal(got)
	}
	for i, p := range got.Points {
		if p.NetworkUnavailable != (i > 0) || p.Average[0] != 10 {
			t.Fatalf("bucket %d: %+v", i, p)
		}
	}
}
