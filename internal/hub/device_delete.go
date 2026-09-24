package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
)

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok || !s.sessionOK(r) {
		writeJSON(w, 401, map[string]string{"error": "login required"})
		return
	}
	id := r.PathValue("agentID")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		ConfirmID string `json:"confirm_id"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || ensureSingleJSONValue(d) != nil || input.ConfirmID != id {
		writeJSON(w, 400, map[string]string{"error": "confirmation must match device ID"})
		return
	}
	tx, err := store.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "delete failed"})
		return
	}
	defer tx.Rollback()
	var revoked bool
	err = tx.QueryRowContext(r.Context(), `SELECT revoked FROM devices WHERE agent_id=? AND agent_id NOT IN (SELECT agent_id FROM deleted_devices)`, id).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "device not found"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "delete failed"})
		return
	}
	if !revoked {
		writeJSON(w, 409, map[string]string{"error": "revoke credentials before deleting history"})
		return
	}
	counts := map[string]int64{}
	// Keep a revoked credential tombstone: legacy credentials cannot recreate it.
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO deleted_devices(agent_id) VALUES(?)`, id); err != nil {
		writeJSON(w, 500, map[string]string{"error": "delete failed"})
		return
	}
	for _, table := range []string{"metric_samples", "disk_snapshots", "disk_events", "enrollments", "servers"} {
		res, err := tx.ExecContext(r.Context(), `DELETE FROM `+table+` WHERE agent_id=?`, id)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "delete failed"})
			return
		}
		n, err := res.RowsAffected()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "delete failed"})
			return
		}
		counts[table] = n
	}
	if tx.Commit() != nil {
		writeJSON(w, 500, map[string]string{"error": "delete failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "deleted", "deleted": counts})
}
