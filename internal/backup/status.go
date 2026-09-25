package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Status struct {
	State      string     `json:"state"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	File       string     `json:"file,omitempty"`
	Keep       int        `json:"keep"`
	Removed    int        `json:"removed"`
}

func writeStatus(dir string, s Status) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".status-*.partial")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = json.NewEncoder(f).Encode(s); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "status.json"))
}

// CreateObserved records the latest attempt, not a claim about all backup files.
// One writer per dedicated backup directory is required.
func CreateObserved(ctx context.Context, source, dir string, keep int) (string, int, error) {
	status := Status{State: "running", StartedAt: time.Now().UTC(), Keep: keep}
	if err := writeStatus(dir, status); err != nil {
		return "", 0, fmt.Errorf("write backup status: %w", err)
	}
	path, removed, err := Create(ctx, source, dir, keep)
	end := time.Now().UTC()
	status.FinishedAt = &end
	status.Removed = removed
	status.State = "success"
	if err != nil {
		status.State = "failed"
	}
	if path != "" {
		status.File = filepath.Base(path)
	}
	statusErr := writeStatus(dir, status)
	return path, removed, errors.Join(err, statusErr)
}

type Overview struct {
	Configured     bool    `json:"configured"`
	Latest         *Status `json:"latest"`
	StatusReadable bool    `json:"status_readable"`
	Files          int     `json:"files"`
	Bytes          int64   `json:"bytes"`
}

func Inspect(dir string) (Overview, error) {
	result := Overview{Configured: dir != ""}
	if dir == "" {
		return result, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "seesize-") || !strings.HasSuffix(name, ".db") {
			continue
		}
		if _, err := time.Parse("20060102T150405.000000000Z", strings.TrimSuffix(strings.TrimPrefix(name, "seesize-"), ".db")); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return result, err
		}
		result.Files++
		result.Bytes += info.Size()
	}
	f, err := os.Open(filepath.Join(dir, "status.json"))
	if err != nil {
		return result, nil
	}
	defer f.Close()
	var status Status
	if err := json.NewDecoder(f).Decode(&status); err != nil {
		return result, nil
	}
	if status.State != "running" && status.State != "failed" && status.State != "success" {
		return result, nil
	}
	if status.StartedAt.IsZero() || status.Keep < 1 || status.Keep > 365 || (status.State != "running" && status.FinishedAt == nil) {
		return result, nil
	}
	result.Latest = &status
	result.StatusReadable = true
	return result, nil
}
