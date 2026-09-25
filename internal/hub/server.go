package hub

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/seesize/seesize/internal/model"
)

//go:embed web/*.html
var webFiles embed.FS

type Server struct {
	auth            *authState
	secureCookies   bool
	backupDir       string
	adminToken      string
	token           string
	store           MetricStore
	now             func() time.Time
	growthThreshold int64
}

func NewServer(token string, store MetricStore) (*Server, error) {
	if store == nil {
		return nil, errors.New("store must not be nil")
	}
	return &Server{token: token, store: store, now: time.Now, growthThreshold: 100 << 20}, nil
}

func (s *Server) SetGrowthThreshold(bytes int64) error {
	if bytes <= 0 {
		return errors.New("growth threshold must be positive")
	}
	s.growthThreshold = bytes
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /api/v1/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/logout", s.handleLogout)
	mux.HandleFunc("GET /api/v1/devices", s.handleDevices)
	mux.HandleFunc("GET /api/v1/backup-status", s.handleBackupStatus)
	mux.HandleFunc("POST /api/v1/enrollments", s.handleEnrollment)
	mux.HandleFunc("POST /api/v1/devices/{agentID}/revoke", s.handleRevoke)
	mux.HandleFunc("DELETE /api/v1/devices/{agentID}", s.handleDeleteDevice)
	mux.HandleFunc("POST /api/v1/agents/register", s.handleRegister)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/agents/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("GET /api/v1/servers", s.handleServers)
	mux.HandleFunc("GET /api/v1/servers/{agentID}/events", s.handleEvents)
	mux.HandleFunc("POST /api/v1/servers/{agentID}/events/{eventID}/ack", s.handleAck)
	mux.HandleFunc("POST /api/v1/agents/disk-snapshots", s.handleDiskUpload)
	mux.HandleFunc("GET /api/v1/servers/{agentID}/disk", s.handleDisk)
	mux.HandleFunc("GET /api/v1/servers/{agentID}/metrics", s.handleHistory)
	mux.HandleFunc("GET /api/v1/servers/{agentID}/trend", s.handleTrend)
	mux.HandleFunc("GET /", s.handleIndex)
	return securityHeaders(s.protect(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid agent credential"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var heartbeat model.Heartbeat
	if err := decoder.Decode(&heartbeat); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid heartbeat: " + err.Error()})
		return
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if heartbeat.ProtocolVersion != model.ProtocolVersion {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported protocol version"})
		return
	}
	if strings.TrimSpace(heartbeat.AgentID) == "" || strings.TrimSpace(heartbeat.Hostname) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_id and hostname are required"})
		return
	}
	if heartbeat.CollectedAt.IsZero() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "collected_at is required"})
		return
	}
	if !s.authorizedFor(r, heartbeat.AgentID) {
		writeJSON(w, 401, map[string]string{"error": "credential does not belong to this agent"})
		return
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if err := s.store.Upsert(r.Context(), heartbeat, host, s.now().UTC()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to store heartbeat"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.store.List(r.Context(), s.now().UTC())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load servers"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	now := s.now().UTC()
	from, err := queryTime(r, "from", now.Add(-time.Hour))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	to, err := queryTime(r, "to", now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if to.Before(from) || to.Sub(from) > 31*24*time.Hour {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "time range must be ordered and no longer than 31 days"})
		return
	}
	limit := 1000
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 5000 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be between 1 and 5000"})
			return
		}
	}
	samples, err := s.store.History(r.Context(), r.PathValue("agentID"), from, to, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "unable to load metrics"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id": r.PathValue("agentID"),
		"from":     from,
		"to":       to,
		"samples":  samples,
	})
}

func queryTime(r *http.Request, name string, fallback time.Time) (time.Time, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must use RFC3339 format", name)
	}
	return value.UTC(), nil
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "web interface unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (s *Server) authorized(r *http.Request) bool {
	return s.authorizedFor(r, "")
}
func (s *Server) authorizedLegacy(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	provided := strings.TrimPrefix(header, prefix)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("invalid trailing data: %w", err)
	}
	return errors.New("request must contain one JSON object")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
