// Package serve is the minimal v0.1 HTTP control plane: one in-memory
// boolean flag, a health check, and a toggle. SQLite persistence lands in
// W1–2 once the store driver dependency is approved. Keep this small —
// the evaluator is the product, the UI is a convenience.
package serve

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// flag is the wire shape of a flag in list/toggle responses.
type flag struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// Server holds in-memory flag state guarded by a mutex.
type Server struct {
	mu    sync.RWMutex
	flags map[string]bool
}

// New returns a Server seeded with one boolean flag, off, so a fresh
// deploy has something to toggle and the 90-second demo works out of the box.
func New() *Server {
	return &Server{flags: map[string]bool{"welcome-banner": false}}
}

// Handler wires the routes: GET /healthz, GET /flags, POST /flags/{key}/toggle.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/flags", s.handleList)
	mux.HandleFunc("/flags/", s.handleFlag)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleList(w http.ResponseWriter, _ *http.Request) {
	s.mu.RLock()
	out := make([]flag, 0, len(s.flags)) // non-nil so JSON emits [] not null
	for k, v := range s.flags {
		out = append(out, flag{Key: k, Enabled: v})
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	writeJSON(w, http.StatusOK, map[string]any{"flags": out})
}

func (s *Server) handleFlag(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "flags" || parts[2] != "toggle" || r.Method != http.MethodPost {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	key := parts[1]

	s.mu.Lock()
	enabled, ok := s.flags[key]
	if ok {
		s.flags[key] = !enabled
		enabled = s.flags[key]
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, `{"error":"flag not found"}`, http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, flag{Key: key, Enabled: enabled})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
