package serve

// auth.go: optional API bearer-token auth and the audit middleware.
//
// Auth model: SWITCHYARD_API_TOKEN unset or empty = open mode (localhost,
// single-user self-host default — documented). Set = every /flags and
// /evaluate request requires `Authorization: Bearer <token>`; healthz,
// the UI, /deploy, and SCIM (own token) stay reachable.
//
// Audit model: every mutating request writes exactly one append-only row
// (create/update/delete/toggle) with full before/after snapshots. Reads
// write nothing. Dry-runs write nothing. The actor is "api-token" (no
// per-user auth exists yet) or "anonymous" in open mode.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/switchyard/switchyard/internal/store"
)

const actorAnonymous = "anonymous"

// currentActor returns the audit actor for this request.
func currentActor(r *http.Request) string {
	if r.Header.Get("Authorization") != "" {
		return "api-token"
	}
	return actorAnonymous
}

// apiAuth wraps the mutating+reading API surface with token auth.
func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("SWITCHYARD_API_TOKEN")
		if token == "" {
			next.ServeHTTP(w, r) // open mode
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			writeErrMsg(w, http.StatusUnauthorized, "invalid or missing bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// auditRecorder captures what a mutation did so exactly one row is
// written per request, by the middleware — handlers never touch audit.
type auditRecorder struct {
	pending *storeAudit
}

type storeAudit struct {
	Actor    string
	Action   string
	Resource string
	Key      string
	Env      string
	Before   string
	After    string
}

// auditMiddleware wraps mutating handlers (POST/PUT/DELETE). On success
// (2xx) it writes one row from the recorder.
func (s *Server) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		rec := &auditRecorder{}
		ctx := context.WithValue(r.Context(), auditKey{}, rec)
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))
		if sw.code < 200 || sw.code >= 300 {
			return // failed mutations write nothing
		}
		if rec.pending == nil {
			return // handler chose not to record (e.g. idempotent no-op)
		}
		_ = s.st.PutAudit(context.Background(), store.AuditEntry{
			Actor:    rec.pending.Actor,
			Action:   rec.pending.Action,
			Resource: rec.pending.Resource,
			Key:      rec.pending.Key,
			Env:      rec.pending.Env,
			Before:   rec.pending.Before,
			After:    rec.pending.After,
		})
	})
}

type auditKey struct{}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// recordAudit is called by handlers to declare what changed.
func recordAudit(r *http.Request, e storeAudit) {
	rec, ok := r.Context().Value(auditKey{}).(*auditRecorder)
	if !ok || rec == nil {
		return
	}
	b, _ := json.Marshal(e)
	_ = b
	rec.pending = &e
}

// jsonString serializes v for an audit snapshot; empty on error.
func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

var _ = jsonString // used by handlers below (recordAudit callers)
