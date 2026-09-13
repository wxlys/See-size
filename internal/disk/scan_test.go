package disk

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanDepthAndGrowth(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "logs", "nested")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "app.log")
	if err := os.WriteFile(file, []byte("123"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := Scan(context.Background(), root, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Complete || len(a.Nodes) != 2 || a.Nodes[0].Bytes != 3 {
		t.Fatalf("unexpected snapshot: %+v", a)
	}
	if err := os.WriteFile(file, []byte("12345678"), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := Scan(context.Background(), root, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	changes, ok := Compare(a, b)
	if !ok || len(changes) != 2 || changes[0].Delta != 5 {
		t.Fatalf("unexpected comparison: %+v", changes)
	}
	limited, err := Scan(context.Background(), root, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if limited.Complete {
		t.Fatal("limit must be incomplete")
	}
	if _, ok := Compare(a, limited); ok {
		t.Fatal("partial comparison allowed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := Scan(ctx, root, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if c.Complete {
		t.Fatal("cancelled scan complete")
	}
}
