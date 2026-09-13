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

func TestLoginEnrollmentBindingRevocationLogout(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "auth.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app, _ := NewServer("legacy", store)
	app.SetAdminToken("independent-admin-secret")
	app.EnableAuthentication()
	handler := app.Handler()
	call := func(method, path string, body any, cookie *http.Cookie, token string, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		data, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(data))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if csrf {
			req.Header.Set("X-SeeSize-Request", "1")
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	if call("GET", "/api/v1/servers", nil, nil, "", false).Code != 401 {
		t.Fatal("unauthenticated read")
	}
	if call("POST", "/api/v1/login", map[string]string{"credential": "independent-admin-secret"}, nil, "", false).Code != 403 {
		t.Fatal("login CSRF")
	}
	login := call("POST", "/api/v1/login", map[string]string{"credential": "independent-admin-secret"}, nil, "", true)
	if login.Code != 200 {
		t.Fatal(login.Body.String())
	}
	cookie := login.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("unsafe cookie")
	}
	if call("GET", "/api/v1/servers", nil, cookie, "", false).Code != 200 {
		t.Fatal("session rejected")
	}
	res := call("POST", "/api/v1/enrollments", map[string]string{"agent_id": "one"}, cookie, "", true)
	if res.Code != 201 {
		t.Fatal(res.Body.String())
	}
	var code map[string]string
	json.Unmarshal(res.Body.Bytes(), &code)
	registration := map[string]string{"agent_id": "one", "code": code["code"]}
	res = call("POST", "/api/v1/agents/register", registration, nil, "", false)
	if res.Code != 201 {
		t.Fatal(res.Body.String())
	}
	var device map[string]string
	json.Unmarshal(res.Body.Bytes(), &device)
	if call("POST", "/api/v1/agents/register", registration, nil, "", false).Code != 401 {
		t.Fatal("registration code reused")
	}
	beat := map[string]any{"agent_id": "one", "hostname": "one", "protocol_version": "v1", "collected_at": time.Now()}
	if call("POST", "/api/v1/agents/heartbeat", beat, nil, device["token"], false).Code != 202 {
		t.Fatal("bound token rejected")
	}
	beat["agent_id"] = "two"
	if call("POST", "/api/v1/agents/heartbeat", beat, nil, device["token"], false).Code != 401 {
		t.Fatal("cross-device write")
	}
	beat["agent_id"] = "one"
	if call("POST", "/api/v1/devices/one/revoke", nil, cookie, "", false).Code != 403 {
		t.Fatal("revoke CSRF")
	}
	if call("POST", "/api/v1/devices/one/revoke", nil, cookie, "", true).Code != 200 {
		t.Fatal("revoke failed")
	}
	for _, token := range []string{device["token"], "legacy"} {
		if call("POST", "/api/v1/agents/heartbeat", beat, nil, token, false).Code != 401 {
			t.Fatal("revocation bypass")
		}
	}
	call("POST", "/api/v1/logout", nil, cookie, "", true)
	if call("GET", "/api/v1/servers", nil, cookie, "", false).Code != 401 {
		t.Fatal("logout not invalidated")
	}
}
