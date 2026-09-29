package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/model"
	"github.com/switchyard/switchyard/internal/store"
)

// newServer opens a store in a temp dir and builds a Server over it.
func newServer(t *testing.T) (*Server, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "switchyard.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st), dbPath
}

// reopen builds a Server over a fresh Store handle on the same DB file —
// simulates a process restart.
func reopen(t *testing.T, dbPath string) *Server {
	t.Helper()
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st)
}

func req(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	r := httptest.NewRequest(method, path, &buf)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rr.Body.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
}

func TestHealthz(t *testing.T) {
	srv, _ := newServer(t)
	rr := req(t, srv.Handler(), http.MethodGet, "/healthz", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok"`) {
		t.Fatalf("healthz = %d %s", rr.Code, rr.Body.String())
	}
}

func TestFlagsStartEmpty(t *testing.T) {
	srv, _ := newServer(t)
	rr := req(t, srv.Handler(), http.MethodGet, "/flags", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d", rr.Code)
	}
	var body struct {
		Flags []struct {
			Key string `json:"key"`
		} `json:"flags"`
	}
	decode(t, rr, &body)
	if len(body.Flags) != 0 {
		t.Fatalf("fresh store has %d flags, want 0 — no seed flags when store-backed", len(body.Flags))
	}
}

func TestCreateFlagPersistsAndAppearsInList(t *testing.T) {
	srv, _ := newServer(t)
	rr := req(t, srv.Handler(), http.MethodPost, "/flags", map[string]any{
		"key":  "checkouts-v2",
		"name": "New checkout flow",
		"kind": "boolean",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body.String())
	}
	rr = req(t, srv.Handler(), http.MethodGet, "/flags", nil)
	var body struct {
		Flags []struct {
			Key  string `json:"key"`
			Name string `json:"name"`
		} `json:"flags"`
	}
	decode(t, rr, &body)
	if len(body.Flags) != 1 || body.Flags[0].Key != "checkouts-v2" || body.Flags[0].Name != "New checkout flow" {
		t.Fatalf("list after create = %+v", body.Flags)
	}
}

func TestTogglePersistsAcrossRestart(t *testing.T) {
	srv, dbPath := newServer(t)
	req(t, srv.Handler(), http.MethodPost, "/flags", map[string]any{"key": "welcome-banner", "kind": "boolean"})
	// Toggle off -> on -> off -> on; third toggle leaves it on.
	req(t, srv.Handler(), http.MethodPost, "/flags/welcome-banner/toggle", nil)
	req(t, srv.Handler(), http.MethodPost, "/flags/welcome-banner/toggle", nil)
	rr := req(t, srv.Handler(), http.MethodPost, "/flags/welcome-banner/toggle", nil)
	var tog struct {
		Enabled bool `json:"enabled"`
	}
	decode(t, rr, &tog)
	if !tog.Enabled {
		t.Fatalf("third toggle should be on: %s", rr.Body.String())
	}
	// Restart: state must survive.
	srv2 := reopen(t, dbPath)
	rr = req(t, srv2.Handler(), http.MethodGet, "/flags", nil)
	if !strings.Contains(rr.Body.String(), `"welcome-banner"`) {
		t.Fatalf("flag lost across restart: %s", rr.Body.String())
	}
}

func TestDeleteFlagRemovesIt(t *testing.T) {
	srv, _ := newServer(t)
	req(t, srv.Handler(), http.MethodPost, "/flags", map[string]any{"key": "temp", "kind": "boolean"})
	rr := req(t, srv.Handler(), http.MethodDelete, "/flags/temp", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete = %d", rr.Code)
	}
	rr = req(t, srv.Handler(), http.MethodGet, "/flags", nil)
	if strings.Contains(rr.Body.String(), `"temp"`) {
		t.Fatalf("flag still listed after delete: %s", rr.Body.String())
	}
	rr = req(t, srv.Handler(), http.MethodDelete, "/flags/temp", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("double delete = %d, want 404", rr.Code)
	}
}

func TestPutFlagSetsEnvironmentConfig(t *testing.T) {
	srv, _ := newServer(t)
	req(t, srv.Handler(), http.MethodPost, "/flags", map[string]any{"key": "f", "kind": "boolean"})
	rr := req(t, srv.Handler(), http.MethodPut, "/flags/f", map[string]any{
		"key":  "f",
		"name": "F",
		"kind": "boolean",
		"environments": map[string]any{
			"production": map[string]any{
				"on":      true,
				"rollout": map[string]any{"kind": "percentage", "percentage": 25},
				"rules":   []any{},
			},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("put flag = %d %s", rr.Code, rr.Body.String())
	}
	rr = req(t, srv.Handler(), http.MethodGet, "/flags/f", nil)
	var f model.Flag
	decode(t, rr, &f)
	prod, ok := f.Environments["production"]
	if !ok || !prod.On || prod.Rollout == nil || prod.Rollout.Percentage != 25 {
		t.Fatalf("production config = %+v", prod)
	}
}

func TestEvaluateEndpointOffAndOn(t *testing.T) {
	srv, _ := newServer(t)
	req(t, srv.Handler(), http.MethodPost, "/flags", map[string]any{"key": "f", "kind": "boolean"})
	// Default (no env config) evaluates off.
	rr := req(t, srv.Handler(), http.MethodPost, "/evaluate/f?env=production", map[string]any{
		"userKey": "alice",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("evaluate = %d %s", rr.Code, rr.Body.String())
	}
	var d struct {
		Enabled bool   `json:"enabled"`
		Reason  string `json:"reason"`
	}
	decode(t, rr, &d)
	if d.Enabled || d.Reason != "off" {
		t.Fatalf("fresh flag eval = %+v, want {false off}", d)
	}
	// Enable in production, evaluate again.
	req(t, srv.Handler(), http.MethodPut, "/flags/f", map[string]any{
		"key":  "f",
		"kind": "boolean",
		"environments": map[string]any{
			"production": map[string]any{"on": true},
		},
	})
	rr = req(t, srv.Handler(), http.MethodPost, "/evaluate/f?env=production", map[string]any{"userKey": "alice"})
	decode(t, rr, &d)
	if !d.Enabled || d.Reason != "fallthrough" {
		t.Fatalf("enabled flag eval = %+v, want {true fallthrough}", d)
	}
}

func TestEvaluateUnknownFlag404s(t *testing.T) {
	srv, _ := newServer(t)
	rr := req(t, srv.Handler(), http.MethodPost, "/evaluate/ghost?env=production", map[string]any{"userKey": "a"})
	if rr.Code != http.StatusNotFound {
		t.Fatalf("unknown flag eval = %d, want 404", rr.Code)
	}
}

func TestNewWithNilStorePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New(nil) must panic — catch misuse early")
		}
	}()
	_ = New(nil)
}
