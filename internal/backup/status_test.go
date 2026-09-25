package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestObservedBackupSuccessFailureAndUnknown(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.db")
	dir := filepath.Join(root, "backups")
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range []string{"servers", "metric_samples", "disk_snapshots", "disk_events", "devices", "enrollments"} {
		if _, err := db.Exec("CREATE TABLE " + table + "(id INTEGER)"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, _, err := CreateObserved(context.Background(), source, dir, 1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Inspect(dir)
	if err != nil || !got.StatusReadable || got.Latest.State != "success" || got.Files != 1 || got.Latest.Removed != 1 || got.Bytes <= 0 {
		t.Fatalf("success: %+v %v", got, err)
	}
	if _, _, err := CreateObserved(context.Background(), filepath.Join(root, "missing.db"), dir, 1); err == nil {
		t.Fatal("missing source accepted")
	}
	got, err = Inspect(dir)
	if err != nil || got.Latest.State != "failed" || got.Files != 1 || got.Latest.File != "" {
		t.Fatalf("failure confused with old success: %+v %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = Inspect(dir)
	if err != nil || got.StatusReadable || got.Files != 1 {
		t.Fatalf("corrupt status: %+v %v", got, err)
	}
	if err := os.Remove(filepath.Join(dir, "status.json")); err != nil {
		t.Fatal(err)
	}
	got, err = Inspect(dir)
	if err != nil || got.StatusReadable || got.Latest != nil {
		t.Fatal("legacy status fabricated")
	}
	got, err = Inspect("")
	if err != nil || got.Configured {
		t.Fatal("unconfigured")
	}
}
