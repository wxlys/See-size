// Package disk provides opt-in, bounded directory size snapshots.
package disk

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Node struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}
type Snapshot struct {
	AgentID  string    `json:"agent_id"`
	Root     string    `json:"root"`
	At       time.Time `json:"at"`
	Depth    int       `json:"depth"`
	Complete bool      `json:"complete"`
	Entries  int       `json:"entries"`
	Skipped  int       `json:"skipped"`
	Reason   string    `json:"reason,omitempty"`
	Nodes    []Node    `json:"nodes"`
}

// Scan counts logical regular-file sizes. Links are not followed; hard links
// count once per directory entry. This is deliberately not filesystem usage.
func Scan(ctx context.Context, root string, depth, maxEntries int) (Snapshot, error) {
	return ScanRate(ctx, root, depth, maxEntries, 1000)
}

// ScanRate paces metadata processing. It is not a physical disk bandwidth cap.
func ScanRate(ctx context.Context, root string, depth, maxEntries, entriesPerSecond int) (Snapshot, error) {
	if entriesPerSecond < 1 || entriesPerSecond > 100000 {
		return Snapshot{}, errors.New("scan rate must be 1..100000 entries per second")
	}
	if depth < 0 || depth > 5 || maxEntries < 1 || maxEntries > 100000 {
		return Snapshot{}, errors.New("depth must be 0..5 and entry limit 1..100000")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{Root: root, At: time.Now().UTC(), Depth: depth, Complete: true, Nodes: []Node{}}
	sizes := map[string]int64{".": 0}
	stop := errors.New("scan limit reached")
	started := time.Now()
	err = walkBatches(ctx, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			s.Reason = "time limit or cancellation"
			return stop
		}
		if walkErr != nil {
			s.Skipped++
			return nil
		}
		if path == root && !entry.IsDir() {
			return errors.New("scan root must be a directory")
		}
		wait := time.Until(started.Add(time.Duration(s.Entries) * time.Second / time.Duration(entriesPerSecond)))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.Reason = "time limit or cancellation"
				return stop
			case <-timer.C:
			}
		}
		s.Entries++
		if s.Entries > maxEntries {
			s.Entries--
			s.Reason = "entry limit"
			return stop
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			s.Skipped++
			return nil
		}
		// Never walk the kernel's virtual trees during a root scan.
		if root == string(filepath.Separator) && (rel == "proc" || rel == "sys" || rel == "dev") {
			s.Skipped++
			return fs.SkipDir
		}
		if entry.IsDir() {
			if rel == "." || len(strings.Split(rel, string(filepath.Separator))) <= depth {
				sizes[rel] += 0
			}
			return nil
		}
		info, e := entry.Info()
		if e != nil {
			s.Skipped++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		for parent := filepath.Dir(rel); ; parent = filepath.Dir(parent) {
			if parent == "." || len(strings.Split(parent, string(filepath.Separator))) <= depth {
				sizes[parent] += info.Size()
			}
			if parent == "." {
				break
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return Snapshot{}, err
	}
	s.Complete = err == nil && s.Skipped == 0
	if s.Skipped > 0 && s.Reason == "" {
		s.Reason = "unreadable or excluded entries"
	}
	for path, size := range sizes {
		s.Nodes = append(s.Nodes, Node{Path: filepath.ToSlash(path), Bytes: size})
	}
	sort.Slice(s.Nodes, func(i, j int) bool { return s.Nodes[i].Path < s.Nodes[j].Path })
	return s, nil
}

// Read bounded batches instead of sorting an entire directory into memory.
// Limit recursion to bound open directory handles as well as Go stack usage.
func walkBatches(ctx context.Context, root string, visit fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		return visit(root, nil, err)
	}
	var walk func(string, fs.DirEntry, int) error
	walk = func(path string, entry fs.DirEntry, depth int) error {
		if err := visit(path, entry, nil); err != nil {
			if err == fs.SkipDir {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if depth >= 128 {
			return visit(path, entry, errors.New("traversal depth limit"))
		}
		f, err := os.Open(path)
		if err != nil {
			return visit(path, entry, err)
		}
		defer f.Close()
		for {
			if ctx.Err() != nil {
				return visit(path, entry, ctx.Err())
			}
			children, readErr := f.ReadDir(64)
			for _, child := range children {
				if err := walk(filepath.Join(path, child.Name()), child, depth+1); err != nil {
					return err
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return visit(path, entry, readErr)
			}
		}
	}
	return walk(root, fs.FileInfoToDirEntry(info), 0)
}

type Change struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Delta int64  `json:"delta"`
}

func Compare(previous, current Snapshot) ([]Change, bool) {
	if !previous.Complete || !current.Complete || previous.Root != current.Root || previous.Depth != current.Depth {
		return []Change{}, false
	}
	old := map[string]int64{}
	for _, n := range previous.Nodes {
		old[n.Path] = n.Bytes
	}
	changes := []Change{}
	for _, n := range current.Nodes {
		changes = append(changes, Change{n.Path, n.Bytes, n.Bytes - old[n.Path]})
		delete(old, n.Path)
	}
	for path, size := range old {
		changes = append(changes, Change{path, 0, -size})
	}
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Delta != changes[j].Delta {
			return changes[i].Delta > changes[j].Delta
		}
		return changes[i].Path < changes[j].Path
	})
	return changes, true
}
