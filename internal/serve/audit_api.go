package serve

// audit_api.go: GET /audit — the procurement window into the append-only
// log. JSON by default; ?format=csv streams a quoted CSV with a download
// disposition (JSON snapshots contain commas — proper RFC 4180 quoting,
// not naive comma-join). Both formats read the same store rows the
// docs/audit-log.md schema defines.

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/switchyard/switchyard/internal/store"
)

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErrMsg(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 10000 {
			limit = n
		}
	}
	entries, err := s.st.ListAudit(r.Context(), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if r.URL.Query().Get("format") == "csv" {
		writeAuditCSV(w, entries)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func writeAuditCSV(w http.ResponseWriter, entries []store.AuditEntry) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="switchyard-audit.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "ts", "actor", "action", "resource", "key", "env", "before", "after", "request_id"})
	for _, e := range entries {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.TS,
			e.Actor,
			e.Action,
			e.Resource,
			e.Key,
			e.Env,
			e.Before,
			e.After,
			e.RequestID,
		})
	}
	cw.Flush()
}

// csvSafe is retained for documentation: the before/after snapshots are
// raw JSON strings and MUST go through encoding/csv's quoter, never a
// naive strings.Join. (Time import kept for the RFC3339 contract.)
var _ = time.RFC3339
var _ = strings.TrimSpace
