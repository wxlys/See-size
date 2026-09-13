package hub

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/seesize/seesize/internal/disk"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Event struct {
	ID             int64      `json:"id"`
	AgentID        string     `json:"agent_id"`
	Root           string     `json:"root"`
	Path           string     `json:"path"`
	Delta          int64      `json:"delta"`
	Threshold      int64      `json:"threshold"`
	From           time.Time  `json:"from"`
	To             time.Time  `json:"to"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
}

// Snapshot and events commit together. Equal timestamps are immutable retries;
// out-of-order uploads are retained but cannot change previously emitted events.
func (s *SQLiteStore) SaveDiskEvents(ctx context.Context, snap disk.Snapshot, threshold int64) error {
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous disk.Snapshot
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT snapshot_json FROM disk_snapshots WHERE agent_id=? AND root=? ORDER BY at_ns DESC LIMIT 1`, snap.AgentID, snap.Root).Scan(&old)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if err = json.Unmarshal(old, &previous); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO disk_snapshots(agent_id,root,at_ns,snapshot_json) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, snap.AgentID, snap.Root, snap.At.UnixNano(), body)
	if err != nil {
		return err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if inserted > 0 && snap.At.After(previous.At) {
		for _, alert := range disk.GrowthAlerts(previous, snap, threshold) {
			_, err = tx.ExecContext(ctx, `INSERT INTO disk_events(agent_id,root,path,delta,threshold,from_ns,to_ns) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, snap.AgentID, snap.Root, alert.Path, alert.Delta, threshold, alert.From.UnixNano(), alert.To.UnixNano())
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Server) SetAdminToken(token string) error {
	if token != "" && (len(token) < 16 || token == s.token) {
		return errors.New("admin token must be independent and at least 16 characters")
	}
	s.adminToken = token
	return nil
}
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "event storage unavailable"})
		return
	}
	before := int64(1<<63 - 1)
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 1 {
			writeJSON(w, 400, map[string]string{"error": "invalid cursor"})
			return
		}
	}
	rows, err := store.db.QueryContext(r.Context(), `SELECT id,agent_id,root,path,delta,threshold,from_ns,to_ns,ack_ns FROM disk_events WHERE agent_id=? AND id<? ORDER BY id DESC LIMIT 101`, r.PathValue("agentID"), before)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to read events"})
		return
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var from, to int64
		var ack sql.NullInt64
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Root, &e.Path, &e.Delta, &e.Threshold, &from, &to, &ack); err != nil {
			writeJSON(w, 500, map[string]string{"error": "unable to read events"})
			return
		}
		e.From = time.Unix(0, from).UTC()
		e.To = time.Unix(0, to).UTC()
		if ack.Valid {
			at := time.Unix(0, ack.Int64).UTC()
			e.AcknowledgedAt = &at
		}
		events = append(events, e)
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to read events"})
		return
	}
	var cursor int64
	if len(events) > 100 {
		events = events[:100]
		cursor = events[99].ID
	}
	writeJSON(w, 200, map[string]any{"events": events, "next_cursor": cursor, "ack_enabled": s.adminToken != ""})
}
func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	if s.adminToken == "" {
		writeJSON(w, 503, map[string]string{"error": "management credential not configured"})
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !s.sessionOK(r) && (!strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) != 1) {
		writeJSON(w, 401, map[string]string{"error": "invalid management credential"})
		return
	}
	store, ok := s.store.(*SQLiteStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "event storage unavailable"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("eventID"), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, 400, map[string]string{"error": "invalid event id"})
		return
	}
	result, err := store.db.ExecContext(r.Context(), `UPDATE disk_events SET ack_ns=COALESCE(ack_ns,?) WHERE id=? AND agent_id=?`, s.now().UTC().UnixNano(), id, r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to acknowledge event"})
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to acknowledge event"})
		return
	}
	if n == 0 {
		writeJSON(w, 404, map[string]string{"error": "event not found"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "acknowledged"})
}
