package hub

import (
	"context"
	"encoding/json"
	"github.com/seesize/seesize/internal/disk"
	"net/http"
	"time"
)

type diskStore interface {
	SaveDisk(context.Context, disk.Snapshot) error
	DiskSnapshots(context.Context, string) ([]disk.Snapshot, error)
}

func (s *SQLiteStore) SaveDisk(ctx context.Context, snapshot disk.Snapshot) error {
	body, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO disk_snapshots(agent_id,root,at_ns,snapshot_json) VALUES(?,?,?,?) ON CONFLICT(agent_id,root,at_ns) DO UPDATE SET snapshot_json=excluded.snapshot_json`, snapshot.AgentID, snapshot.Root, snapshot.At.UnixNano(), body)
	return err
}
func (s *SQLiteStore) DiskSnapshots(ctx context.Context, id string) ([]disk.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT snapshot_json FROM disk_snapshots WHERE agent_id=? AND root=(SELECT root FROM disk_snapshots WHERE agent_id=? ORDER BY at_ns DESC LIMIT 1) ORDER BY at_ns DESC LIMIT 2`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []disk.Snapshot{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var snap disk.Snapshot
		if err := json.Unmarshal(body, &snap); err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

// CompleteDiskReference finds a prior complete scan with the same scope.
// It is a display reference only: it never makes a partial scan comparable.
func (s *SQLiteStore) CompleteDiskReference(ctx context.Context, current disk.Snapshot) (*disk.Snapshot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT snapshot_json FROM disk_snapshots
		WHERE agent_id=? AND root=? AND at_ns<?
		AND json_extract(snapshot_json,'$.complete')=1
		AND json_extract(snapshot_json,'$.depth')=? ORDER BY at_ns DESC LIMIT 1`,
		current.AgentID, current.Root, current.At.UnixNano(), current.Depth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var body []byte
	if err := rows.Scan(&body); err != nil {
		return nil, err
	}
	var snap disk.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}
func (s *Server) handleDiskUpload(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, 401, map[string]string{"error": "invalid agent credential"})
		return
	}
	store, ok := s.store.(diskStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "disk storage unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var snap disk.Snapshot
	if err := d.Decode(&snap); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid snapshot"})
		return
	}
	if ensureSingleJSONValue(d) != nil || snap.AgentID == "" || snap.Root == "" || snap.At.IsZero() || snap.At.After(time.Now().Add(5*time.Minute)) || snap.Depth < 0 || snap.Depth > 5 || len(snap.Nodes) == 0 || len(snap.Nodes) > 100000 {
		writeJSON(w, 400, map[string]string{"error": "invalid snapshot fields"})
		return
	}
	seen := map[string]bool{}
	if !s.authorizedFor(r, snap.AgentID) {
		writeJSON(w, 401, map[string]string{"error": "credential does not belong to this agent"})
		return
	}
	for _, n := range snap.Nodes {
		if n.Bytes < 0 || n.Path == "" || seen[n.Path] {
			writeJSON(w, 400, map[string]string{"error": "invalid directory nodes"})
			return
		}
		seen[n.Path] = true
	}
	var saveErr error
	if events, ok := store.(interface {
		SaveDiskEvents(context.Context, disk.Snapshot, int64) error
	}); ok {
		saveErr = events.SaveDiskEvents(r.Context(), snap, s.growthThreshold)
	} else {
		saveErr = store.SaveDisk(r.Context(), snap)
	}
	if saveErr != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to save snapshot"})
		return
	}
	writeJSON(w, 202, map[string]string{"status": "accepted"})
}
func (s *Server) handleDisk(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(diskStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "disk storage unavailable"})
		return
	}
	snapshots, err := store.DiskSnapshots(r.Context(), r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to read snapshots"})
		return
	}
	changes := []disk.Change{}
	comparable := false
	alerts := []disk.GrowthAlert{}
	if len(snapshots) == 2 {
		changes, comparable = disk.Compare(snapshots[1], snapshots[0])
		alerts = disk.GrowthAlerts(snapshots[1], snapshots[0], s.growthThreshold)
	}
	var reference *disk.Snapshot
	if len(snapshots) > 0 && !snapshots[0].Complete {
		if refs, ok := store.(interface {
			CompleteDiskReference(context.Context, disk.Snapshot) (*disk.Snapshot, error)
		}); ok {
			reference, err = refs.CompleteDiskReference(r.Context(), snapshots[0])
			if err != nil {
				writeJSON(w, 500, map[string]string{"error": "unable to read complete reference"})
				return
			}
		}
	}
	writeJSON(w, 200, map[string]any{"snapshots": snapshots, "reference_snapshot": reference, "comparable": comparable, "changes": changes, "alerts": alerts, "growth_threshold_bytes": s.growthThreshold})
}
