package hub

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupStatusRequiresLogin(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "hub.db"), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h, cookie := acceptanceApp(t, store, time.Now())
	req := httptest.NewRequest("GET", "/api/v1/backup-status", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 401 {
		t.Fatal("unauthenticated read")
	}
	req.AddCookie(cookie)
	res = httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatal(res.Code)
	}
}
