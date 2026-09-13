package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScanRateAndCancellation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "test"), []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	snap, err := ScanRate(context.Background(), root, 1, 100, 10)
	if err != nil || !snap.Complete {
		t.Fatal(snap, err)
	}
	if time.Since(start) < 100*time.Millisecond {
		t.Fatal("rate not applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	snap, err = ScanRate(ctx, root, 1, 100, 1)
	if err != nil || snap.Complete {
		t.Fatal("expected partial scan", snap, err)
	}
	if _, err := ScanRate(context.Background(), root, 1, 100, 0); err == nil {
		t.Fatal("invalid rate accepted")
	}
}
