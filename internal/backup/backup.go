// Package backup creates verified, compact SQLite snapshots without copying WAL files.
package backup

import (
	"context"
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func open(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source must be a regular database file")
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	uri := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func Verify(ctx context.Context, path string) error {
	db, err := open(path)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return err
		}
		count++
		if status != "ok" {
			return fmt.Errorf("integrity check: %s", status)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("missing integrity result")
	}
	rows.Close()
	for _, table := range []string{"servers", "metric_samples", "disk_snapshots", "disk_events", "devices", "enrollments"} {
		var n int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("not a current SeeSize database: missing %s", table)
		}
	}
	return nil
}

// Copy writes a new destination only. Existing files, including empty files,
// are never replaced. The target becomes visible only after verification.
func Copy(ctx context.Context, source, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("target already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	db, err := open(source)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".seesize-backup-*.partial")
	if err != nil {
		return err
	}
	name := temp.Name()
	temp.Close()
	defer os.Remove(name)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", name); err != nil {
		return err
	}
	if err := Verify(ctx, name); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return err
	}
	// Hard linking on the same filesystem atomically publishes without clobber.
	if err := os.Link(name, target); err != nil {
		return fmt.Errorf("publish verified backup: %w", err)
	}
	return nil
}

func Create(ctx context.Context, source, dir string, keep int) (string, int, error) {
	if keep < 1 || keep > 365 {
		return "", 0, fmt.Errorf("keep must be 1..365")
	}
	target := filepath.Join(dir, "seesize-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".db")
	if err := Copy(ctx, source, target); err != nil {
		return "", 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return target, 0, err
	}
	names := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "seesize-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		if _, err := time.Parse("20060102T150405.000000000Z", strings.TrimSuffix(strings.TrimPrefix(name, "seesize-"), ".db")); err == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	removed := 0
	for len(names) > keep {
		path := filepath.Join(dir, names[0])
		names = names[1:]
		// Do not remove an active source even if it lives in the backup directory.
		src, e1 := os.Stat(source)
		candidate, e2 := os.Stat(path)
		if e1 != nil || e2 != nil {
			return target, removed, fmt.Errorf("unable to validate retention target")
		}
		if os.SameFile(src, candidate) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return target, removed, err
		}
		removed++
	}
	return target, removed, nil
}
