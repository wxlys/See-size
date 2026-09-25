package hub

import (
	"github.com/seesize/seesize/internal/backup"
	"net/http"
)

// SetBackupDirectory configures a trusted local directory before serving.
func (s *Server) SetBackupDirectory(dir string) { s.backupDir = dir }
func (s *Server) handleBackupStatus(w http.ResponseWriter, r *http.Request) {
	if !s.sessionOK(r) {
		writeJSON(w, 401, map[string]string{"error": "login required"})
		return
	}
	result, err := backup.Inspect(s.backupDir)
	if err != nil {
		writeJSON(w, 503, map[string]string{"error": "backup directory unavailable"})
		return
	}
	writeJSON(w, 200, result)
}
