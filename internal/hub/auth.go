package hub

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

type authState struct {
	mu       sync.Mutex
	sessions map[string]time.Time
	window   time.Time
	attempts int
}

// SetSecureCookies must be configured before serving requests. Enable it when
// HTTPS terminates at a reverse proxy; forwarded headers are not trusted.
func (s *Server) SetSecureCookies(enabled bool) {
	s.secureCookies = enabled
}

func secret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func (s *Server) EnableAuthentication() error {
	if len(s.adminToken) < 16 {
		return errors.New("SEE_SIZE_ADMIN_TOKEN is required (at least 16 characters)")
	}
	s.auth = &authState{sessions: map[string]time.Time{}}
	return nil
}
func (s *Server) sessionOK(r *http.Request) bool {
	if s.auth == nil {
		return false
	}
	cookie, err := r.Cookie("seesize_session")
	if err != nil {
		return false
	}
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	expires, ok := s.auth.sessions[digest(cookie.Value)]
	return ok && s.now().Before(expires)
}
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if s.auth == nil || r.URL.Path == "/healthz" || r.URL.Path == "/login" || r.URL.Path == "/api/v1/login" || strings.HasPrefix(r.URL.Path, "/api/v1/agents/") {
			next.ServeHTTP(w, r)
			return
		}
		if !s.sessionOK(r) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeJSON(w, 401, map[string]string{"error": "login required"})
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-SeeSize-Request") != "1" {
			writeJSON(w, 403, map[string]string{"error": "missing request protection"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, 503, map[string]string{"error": "login not enabled"})
		return
	}
	if r.Header.Get("X-SeeSize-Request") != "1" {
		writeJSON(w, 403, map[string]string{"error": "missing request protection"})
		return
	}
	s.auth.mu.Lock()
	now := s.now()
	if now.Sub(s.auth.window) >= time.Minute {
		s.auth.window = now
		s.auth.attempts = 0
	}
	s.auth.attempts++
	limited := s.auth.attempts > 20
	s.auth.mu.Unlock()
	if limited {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]string{"error": "too many attempts"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		Credential string `json:"credential"`
	}
	dec := json.NewDecoder(r.Body)
	if dec.Decode(&input) != nil || ensureSingleJSONValue(dec) != nil || subtle.ConstantTimeCompare([]byte(digest(input.Credential)), []byte(digest(s.adminToken))) != 1 {
		writeJSON(w, 401, map[string]string{"error": "invalid credential"})
		return
	}
	token := secret()
	s.auth.mu.Lock()
	for k, t := range s.auth.sessions {
		if !now.Before(t) {
			delete(s.auth.sessions, k)
		}
	}
	if len(s.auth.sessions) >= 128 {
		s.auth.mu.Unlock()
		writeJSON(w, 429, map[string]string{"error": "session limit reached"})
		return
	}
	s.auth.sessions[digest(token)] = now.Add(8 * time.Hour)
	s.auth.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "seesize_session", Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
	writeJSON(w, 200, map[string]string{"status": "logged in"})
}
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.auth != nil {
		if c, e := r.Cookie("seesize_session"); e == nil {
			s.auth.mu.Lock()
			delete(s.auth.sessions, digest(c.Value))
			s.auth.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "seesize_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookies || r.TLS != nil, SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]string{"status": "logged out"})
}
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	body, err := webFiles.ReadFile("web/login.html")
	if err != nil {
		http.Error(w, "login unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}
