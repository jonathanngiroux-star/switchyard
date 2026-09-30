package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

func auditTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "audit-api.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st
}

func TestAuditEndpointReturnsJSON(t *testing.T) {
	s, st := auditTestServer(t)
	h := s.Handler()
	// Generate three mutations.
	for _, body := range []string{
		`{"key":"f1","kind":"boolean"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", w.Code, w.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/flags/f1/toggle?env=production", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle = %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/audit", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /audit = %d %s", w.Code, w.Body.String())
	}
	var payload struct {
		Entries []struct {
			ID     int64  `json:"id"`
			TS     string `json:"ts"`
			Actor  string `json:"actor"`
			Action string `json:"action"`
			Key    string `json:"key"`
			Env    string `json:"env"`
			Before string `json:"before"`
			After  string `json:"after"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, w.Body.String())
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(payload.Entries))
	}
	if payload.Entries[0].Action != "create" || payload.Entries[1].Action != "toggle" {
		t.Fatalf("actions = %+v", payload.Entries)
	}
	if payload.Entries[1].Env != "production" || payload.Entries[1].Before == "" {
		t.Fatalf("toggle entry = %+v", payload.Entries[1])
	}
	// TS is RFC3339 (store-written).
	if payload.Entries[0].TS == "" {
		t.Fatal("ts missing")
	}
	_ = st
}

func TestAuditEndpointCSVExport(t *testing.T) {
	s, _ := auditTestServer(t)
	h := s.Handler()
	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(`{"key":"csv-flag","kind":"boolean"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(httptest.NewRecorder(), req)

	req = httptest.NewRequest(http.MethodGet, "/audit?format=csv", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CSV = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("content type = %s", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "switchyard-audit") {
		t.Fatalf("disposition = %s", cd)
	}
	body := w.Body.String()
	if !strings.Contains(body, "id,ts,actor,action,resource,key,env,before,after,request_id") {
		t.Fatalf("CSV header missing:\n%s", body)
	}
	// CSV quoting: JSON snapshots contain commas and must be quoted.
	if !strings.Contains(body, `"`) {
		t.Fatalf("CSV must quote JSON snapshots:\n%s", body)
	}
	lines := strings.Split(strings.TrimSpace(body), "\n")
	if len(lines) < 2 {
		t.Fatalf("CSV needs header + rows, got %d lines", len(lines))
	}
}

func TestAuditEndpointRespectsLimitParam(t *testing.T) {
	s, _ := auditTestServer(t)
	h := s.Handler()
	// Create 5 flags.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(`{"key":"lim-`+string(rune('a'+i))+`","kind":"boolean"}`))
		req.Header.Set("Content-Type", "application/json")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	req := httptest.NewRequest(http.MethodGet, "/audit?limit=2", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var payload struct {
		Entries []json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Entries) != 2 {
		t.Fatalf("limit=2 returned %d entries", len(payload.Entries))
	}
}
