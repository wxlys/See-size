package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveWALRestoreAndRetention(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "live.db")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"servers", "metric_samples", "disk_snapshots", "disk_events", "devices", "enrollments"} {
		if _, err := db.Exec("CREATE TABLE " + name + "(id INTEGER PRIMARY KEY, value TEXT)"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO servers VALUES(1,'in WAL')"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dir := filepath.Join(root, "backups")
	var last string
	for i := 0; i < 3; i++ {
		last, _, err = Create(ctx, source, dir, 2)
		if err != nil {
			t.Fatal(err)
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 2 {
		t.Fatal("retention failed", files, err)
	}
	restored := filepath.Join(root, "restored.db")
	if err := Copy(ctx, last, restored); err != nil {
		t.Fatal(err)
	}
	restoredDB, err := open(restored)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDB.Close()
	var value string
	if err := restoredDB.QueryRow("SELECT value FROM servers WHERE id=1").Scan(&value); err != nil || value != "in WAL" {
		t.Fatal("WAL not preserved", value, err)
	}
	if err := Copy(ctx, last, restored); err == nil {
		t.Fatal("overwrote existing destination")
	}
	bad := filepath.Join(root, "bad.db")
	if err := os.WriteFile(bad, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Copy(ctx, bad, filepath.Join(root, "bad-restore.db")); err == nil {
		t.Fatal("corrupt input accepted")
	}
	files, _ = os.ReadDir(dir)
	if len(files) != 2 {
		t.Fatal("backup set altered")
	}
}
