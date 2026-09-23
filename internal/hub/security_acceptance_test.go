package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestSecurityExpiryAndLoginThrottle(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "security.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, err := NewServer("", store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	if err := app.SetAdminToken("security-test-admin"); err != nil {
		t.Fatal(err)
	}
	if err := app.EnableAuthentication(); err != nil {
		t.Fatal(err)
	}
	handler := app.Handler()
	call := func(method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		req.Header.Set("X-SeeSize-Request", "1")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	login := func() *http.Cookie {
		t.Helper()
		res := call("POST", "/api/v1/login", map[string]string{"credential": "security-test-admin"}, nil)
		if res.Code != 200 {
			t.Fatalf("login: %d", res.Code)
		}
		return res.Result().Cookies()[0]
	}
	cookie := login()
	res := call("POST", "/api/v1/enrollments", map[string]string{"agent_id": "expiry-test"}, cookie)
	if res.Code != 201 {
		t.Fatalf("enrollment: %d", res.Code)
	}
	var enrollment map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &enrollment); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	if got := call("POST", "/api/v1/agents/register", map[string]any{"agent_id": "expiry-test", "code": enrollment["code"]}, nil).Code; got != 401 {
		t.Fatalf("expired enrollment: %d", got)
	}
	var devices int
	if err := store.db.QueryRow("SELECT count(*) FROM devices").Scan(&devices); err != nil || devices != 0 {
		t.Fatalf("expired enrollment created device: %d %v", devices, err)
	}
	now = now.Add(7*time.Hour + 50*time.Minute - time.Nanosecond)
	if got := call("GET", "/api/v1/servers", nil, cookie).Code; got != 200 {
		t.Fatalf("session before expiry: %d", got)
	}
	now = now.Add(time.Nanosecond)
	if got := call("GET", "/api/v1/servers", nil, cookie).Code; got != 401 {
		t.Fatalf("session at expiry: %d", got)
	}
	for i := 0; i < 20; i++ {
		res := call("POST", "/api/v1/login", map[string]string{"credential": "wrong"}, nil)
		if res.Code != 401 || len(res.Result().Cookies()) != 0 {
			t.Fatalf("wrong login %d: %d", i, res.Code)
		}
	}
	res = call("POST", "/api/v1/login", map[string]string{"credential": "security-test-admin"}, nil)
	if res.Code != 429 || res.Header().Get("Retry-After") == "" {
		t.Fatal("login throttle missing")
	}
	now = now.Add(time.Minute)
	fresh := login()
	if got := call("GET", "/api/v1/servers", nil, fresh).Code; got != 200 {
		t.Fatalf("recovery: %d", got)
	}
	if got := call("GET", "/api/v1/servers", nil, cookie).Code; got != 401 {
		t.Fatal("expired session revived")
	}
}
