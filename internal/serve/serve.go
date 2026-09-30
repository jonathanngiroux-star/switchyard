// Package serve is the HTTP control plane: flag CRUD, environment config,
// toggling, and local evaluation, all backed by the SQLite store. The
// evaluator is the product; this API is how flags get managed.
package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/switchyard/switchyard/internal/eval"
	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// Server serves the control-plane API over a Store.
type Server struct {
	st *store.Store
}

// New builds a Server over st. A nil store is a programming error.
func New(st *store.Store) *Server {
	if st == nil {
		panic("serve: nil store")
	}
	return &Server{st: st}
}

// Handler wires the routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/flags", s.handleFlags)
	mux.HandleFunc("/flags/", s.handleFlagPath)
	mux.HandleFunc("/evaluate/", s.handleEvaluate)
	s.registerExtras(mux)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleFlags: GET list, POST create.
func (s *Server) handleFlags(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		flags, err := s.st.ListFlags(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"flags": flags})
	case http.MethodPost:
		var f model.Flag
		if !decodeBody(w, r, &f) {
			return
		}
		if f.Key == "" {
			writeErrMsg(w, http.StatusBadRequest, "key is required")
			return
		}
		if f.Environments == nil {
			f.Environments = map[string]model.FlagEnvironment{}
		}
		if _, err := s.st.GetFlag(r.Context(), f.Key); err == nil {
			writeErrMsg(w, http.StatusConflict, "flag already exists")
			return
		} else if err != store.ErrNotFound {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := s.st.PutFlag(r.Context(), f); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusCreated, f)
	default:
		writeErrMsg(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleFlagPath routes /flags/{key} and /flags/{key}/toggle.
func (s *Server) handleFlagPath(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 2 {
		s.handleFlag(w, r, parts[1])
		return
	}
	if len(parts) == 3 && parts[2] == "toggle" {
		s.handleToggle(w, r, parts[1])
		return
	}
	writeErrMsg(w, http.StatusNotFound, "not found")
}

func (s *Server) handleFlag(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodGet:
		f, err := s.st.GetFlag(r.Context(), key)
		if err == store.ErrNotFound {
			writeErrMsg(w, http.StatusNotFound, "flag not found")
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, f)
	case http.MethodPut:
		var f model.Flag
		if !decodeBody(w, r, &f) {
			return
		}
		f.Key = key
		if f.Environments == nil {
			f.Environments = map[string]model.FlagEnvironment{}
		}
		if err := s.st.PutFlag(r.Context(), f); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, f)
	case http.MethodDelete:
		if err := s.st.DeleteFlag(r.Context(), key); err == store.ErrNotFound {
			writeErrMsg(w, http.StatusNotFound, "flag not found")
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": key})
	default:
		writeErrMsg(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleToggle flips a flag's enabled state in one environment.
// Default env is production; override with ?env=.
func (s *Server) handleToggle(w http.ResponseWriter, r *http.Request, key string) {
	if r.Method != http.MethodPost {
		writeErrMsg(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	env := r.URL.Query().Get("env")
	if env == "" {
		env = "production"
	}
	f, err := s.st.GetFlag(r.Context(), key)
	if err == store.ErrNotFound {
		writeErrMsg(w, http.StatusNotFound, "flag not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	fe := f.Environments[env] // zero value: off, no rules
	fe.On = !fe.On
	if err := s.st.SetFlagEnvironment(r.Context(), key, env, fe); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "env": env, "enabled": fe.On})
}

// handleEvaluate: POST /evaluate/{key}?env=production
// body: {"userKey": "...", "attributes": {"k": "v"}}
func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErrMsg(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		writeErrMsg(w, http.StatusNotFound, "not found")
		return
	}
	key := parts[1]
	env := r.URL.Query().Get("env")
	if env == "" {
		env = "production"
	}
	f, err := s.st.GetFlag(r.Context(), key)
	if err == store.ErrNotFound {
		writeErrMsg(w, http.StatusNotFound, "flag not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var ctx eval.Context
	if !decodeBody(w, r, &ctx) {
		return
	}
	segs, err := s.st.ListSegments(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	segMap := make(map[string]model.Segment, len(segs))
	for _, seg := range segs {
		segMap[seg.Key] = seg
	}
	d := eval.Evaluate(f, env, segMap, ctx)
	writeJSON(w, http.StatusOK, d)
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErrMsg(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeErrMsg(w, code, err.Error())
}

func writeErrMsg(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
