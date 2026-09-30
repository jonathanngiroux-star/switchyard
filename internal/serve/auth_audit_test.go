package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

func newAuthServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), st, dbPath
}

func TestNoTokenConfiguredMeansOpenAPI(t *testing.T) {
	// Backward-compat + localhost dev: no SWITCHYARD_API_TOKEN = the API
	// is open (single-user, self-hosted default). Documented, not an
	// accident; the README warns.
	s, _, _ := newAuthServer(t)
	req := httptest.NewRequest(http.MethodGet, "/flags", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("no token configured, GET /flags = %d, want 200 (open mode)", w.Code)
	}
}

func TestTokenConfiguredRequiresAuth(t *testing.T) {
	s, _, _ := newAuthServer(t)
	os.Setenv("SWITCHYARD_API_TOKEN", "secret-1")
	t.Cleanup(func() { os.Unsetenv("SWITCHYARD_API_TOKEN") })

	// No header -> 401.
	req := httptest.NewRequest(http.MethodGet, "/flags", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth = %d, want 401", w.Code)
	}
	// Wrong token -> 401.
	req = httptest.NewRequest(http.MethodGet, "/flags", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", w.Code)
	}
	// Right token -> 200.
	req = httptest.NewRequest(http.MethodGet, "/flags", nil)
	req.Header.Set("Authorization", "Bearer secret-1")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("right token = %d, want 200", w.Code)
	}
	// healthz and / (UI) stay open in auth mode.
	for _, path := range []string{"/healthz", "/"} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		w = httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("open path %s = %d, want 200", path, w.Code)
		}
	}
}

func TestAuditWrittenForEveryMutation(t *testing.T) {
	s, st, _ := newAuthServer(t)
	h := s.Handler()

	// Create -> one row.
	body := `{"key":"audited","kind":"boolean"}`
	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	// Toggle -> second row.
	req = httptest.NewRequest(http.MethodPost, "/flags/audited/toggle?env=production", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("toggle = %d", w.Code)
	}
	// Update -> third row.
	req = httptest.NewRequest(http.MethodPut, "/flags/audited", strings.NewReader(`{"kind":"boolean","environments":{"production":{"on":true}}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put = %d %s", w.Code, w.Body.String())
	}
	// Delete -> fourth row.
	req = httptest.NewRequest(http.MethodDelete, "/flags/audited", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete = %d", w.Code)
	}

	rows, err := st.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("audit rows = %d, want 4 (create/toggle/update/delete)", len(rows))
	}
	actions := []string{rows[0].Action, rows[1].Action, rows[2].Action, rows[3].Action}
	want := []string{"create", "toggle", "update", "delete"}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("actions = %v, want %v", actions, want)
		}
	}
	// Snapshot fields: toggle row carries before/after on-state.
	tg := rows[1]
	if tg.Env != "production" || tg.Before == "" || tg.After == "" {
		t.Fatalf("toggle row = %+v, want env + before/after snapshots", tg)
	}
	// Reads write nothing.
	n := len(rows)
	req = httptest.NewRequest(http.MethodGet, "/flags", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	rows2, _ := st.ListAudit(context.Background(), 100)
	if len(rows2) != n {
		t.Fatalf("GET wrote audit rows: %d -> %d", n, len(rows2))
	}
}

func TestAuditActorFromAPIKeyWhenConfigured(t *testing.T) {
	s, st, _ := newAuthServer(t)
	os.Setenv("SWITCHYARD_API_TOKEN", "secret-2")
	t.Cleanup(func() { os.Unsetenv("SWITCHYARD_API_TOKEN") })
	h := s.Handler()

	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(`{"key":"actor-test","kind":"boolean"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-2")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d", w.Code)
	}
	rows, _ := st.ListAudit(context.Background(), 100)
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	if rows[0].Actor != "api-token" {
		t.Fatalf("actor = %q, want api-token", rows[0].Actor)
	}
}

func TestDeployIDEpisode(t *testing.T) {
	// /deploy returns the stable deploy ID for the evidence loop.
	s, _, _ := newAuthServer(t)
	h := s.Handler()
	req := httptest.NewRequest(http.MethodGet, "/deploy", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("deploy = %d", w.Code)
	}
	var d1 struct {
		DeployID string `json:"deploy_id"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &d1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d1.DeployID) != 36 || d1.Version == "" {
		t.Fatalf("deploy payload = %+v", d1)
	}
	// Stable within the process.
	req = httptest.NewRequest(http.MethodGet, "/deploy", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var d2 struct {
		DeployID string `json:"deploy_id"`
	}
	json.Unmarshal(w.Body.Bytes(), &d2)
	if d1.DeployID != d2.DeployID {
		t.Fatalf("deploy id unstable: %q vs %q", d1.DeployID, d2.DeployID)
	}
}
