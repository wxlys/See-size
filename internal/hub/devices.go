package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleEnrollment(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok || !s.sessionOK(r) {
		writeJSON(w, 401, map[string]string{"error": "login required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		AgentID string `json:"agent_id"`
	}
	d := json.NewDecoder(r.Body)
	if d.Decode(&input) != nil || ensureSingleJSONValue(d) != nil || strings.TrimSpace(input.AgentID) == "" || len(input.AgentID) > 128 {
		writeJSON(w, 400, map[string]string{"error": "agent id required, max 128 characters"})
		return
	}
	code := secret()
	expires := s.now().Add(10 * time.Minute)
	// One current code per Agent ID; issuing another code invalidates the old one.
	_, err := store.db.ExecContext(r.Context(), `INSERT INTO enrollments(agent_id,code_hash,expires_ns) VALUES(?,?,?) ON CONFLICT(agent_id) DO UPDATE SET code_hash=excluded.code_hash,expires_ns=excluded.expires_ns`, input.AgentID, digest(code), expires.UnixNano())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "unable to create enrollment"})
		return
	}
	writeJSON(w, 201, map[string]any{"agent_id": input.AgentID, "code": code, "expires_at": expires})
}
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "storage unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		AgentID string `json:"agent_id"`
		Code    string `json:"code"`
	}
	d := json.NewDecoder(r.Body)
	if d.Decode(&input) != nil || ensureSingleJSONValue(d) != nil || len(input.Code) != 64 {
		writeJSON(w, 400, map[string]string{"error": "invalid enrollment"})
		return
	}
	tx, err := store.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "storage unavailable"})
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `DELETE FROM enrollments WHERE agent_id=? AND code_hash=? AND expires_ns>?`, input.AgentID, digest(input.Code), s.now().UnixNano())
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "registration failed"})
		return
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		writeJSON(w, 401, map[string]string{"error": "code invalid or expired"})
		return
	}
	token := secret()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO devices(agent_id,token_hash,revoked) VALUES(?,?,0) ON CONFLICT(agent_id) DO UPDATE SET token_hash=excluded.token_hash,revoked=0`, input.AgentID, digest(token))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "registration failed"})
		return
	}
	if tx.Commit() != nil {
		writeJSON(w, 500, map[string]string{"error": "registration failed"})
		return
	}
	writeJSON(w, 201, map[string]string{"agent_id": input.AgentID, "token": token})
}
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok {
		writeJSON(w, 503, map[string]string{"error": "storage unavailable"})
		return
	}
	rows, err := store.db.QueryContext(r.Context(), `SELECT agent_id,revoked FROM devices ORDER BY agent_id`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "storage unavailable"})
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id string
		var revoked bool
		if rows.Scan(&id, &revoked) != nil {
			writeJSON(w, 500, map[string]string{"error": "read failed"})
			return
		}
		out = append(out, map[string]any{"agent_id": id, "revoked": revoked})
	}
	if rows.Err() != nil {
		writeJSON(w, 500, map[string]string{"error": "read failed"})
		return
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}
func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(*SQLiteStore)
	if !ok || !s.sessionOK(r) {
		writeJSON(w, 401, map[string]string{"error": "login required"})
		return
	}
	tx, err := store.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "revoke failed"})
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), `UPDATE devices SET revoked=1 WHERE agent_id=?`, r.PathValue("agentID"))
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "revoke failed"})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		writeJSON(w, 404, map[string]string{"error": "device not found"})
		return
	}
	if _, err = tx.ExecContext(r.Context(), `DELETE FROM enrollments WHERE agent_id=?`, r.PathValue("agentID")); err != nil {
		writeJSON(w, 500, map[string]string{"error": "revoke failed"})
		return
	}
	if tx.Commit() != nil {
		writeJSON(w, 500, map[string]string{"error": "revoke failed"})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "revoked"})
}
func (s *Server) authorizedFor(r *http.Request, id string) bool {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if store, ok := s.store.(*SQLiteStore); ok {
		var count int
		query := `SELECT count(*) FROM devices WHERE token_hash=? AND revoked=0`
		args := []any{digest(token)}
		if id != "" {
			query += ` AND agent_id=?`
			args = append(args, id)
		}
		if err := store.db.QueryRowContext(r.Context(), query, args...).Scan(&count); err != nil {
			return false
		}
		if count == 1 {
			return true
		}
		if id != "" {
			if err := store.db.QueryRowContext(r.Context(), `SELECT count(*) FROM devices WHERE agent_id=?`, id).Scan(&count); err != nil || count > 0 {
				return false
			}
		}
	}
	return s.token != "" && s.authorizedLegacy(r)
}
