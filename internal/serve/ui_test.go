package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

func TestSCIMMountedOnlyWithToken(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ui.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	// No token: /scim must 404 (not mounted).
	h := New(st).Handler()
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("without token, /scim = %d, want 404 (not mounted)", w.Code)
	}

	// With token: mounted and auth-gated.
	os.Setenv("SWITCHYARD_SCIM_TOKEN", "tok-1")
	t.Cleanup(func() { os.Unsetenv("SWITCHYARD_SCIM_TOKEN") })
	h2 := New(st).Handler()
	req = httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
	w = httptest.NewRecorder()
	h2.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("with token but no auth header = %d, want 401", w.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/scim/v2/ServiceProviderConfig", nil)
	req.Header.Set("Authorization", "Bearer tok-1")
	w = httptest.NewRecorder()
	h2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("with token + auth = %d, want 200", w.Code)
	}
}

func TestEmbeddedUIIsServed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ui.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	h := New(st).Handler()

	rr := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, rr)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"<!DOCTYPE html>", "switchyard", "/flags"} {
		if !strings.Contains(body, want) {
			t.Fatalf("UI missing %q", want)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("content type = %s", w.Header().Get("Content-Type"))
	}
}

// The UI must escape every value interpolated into innerHTML. A flag
// key arriving from the API (or a stale store from before server-side
// validation) must never reach the DOM as markup. This pins the
// escaping calls; key_validation_test.go pins the API boundary.
func TestEmbeddedUIEscapesInterpolatedValues(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ui3.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	h := New(st).Handler()

	rr := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, rr)
	body := w.Body.String()
	for _, want := range []string{
		"function esc(s)",
		"esc(f.key)",
		"esc(f.kind)",
		"JSON.stringify(f.key)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("UI missing %q — escaping regressed", want)
		}
	}
	// The pre-fix UI interpolated raw keys ("..." + f.key + "..."); no
	// bare "+ f.key" may remain anywhere — esc( and JSON.stringify(
	// are the only sanctioned uses of f.key.
	if strings.Contains(body, "+ f.key") {
		t.Fatal("UI still interpolates raw f.key")
	}
}

func TestUICanToggleThroughAPI(t *testing.T) {
	// The UI is a static page over the JSON API; this pins the API the UI
	// depends on: create, toggle, list.
	dbPath := filepath.Join(t.TempDir(), "ui2.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	h := New(st).Handler()

	body := `{"key":"ui-flag","kind":"boolean"}`
	req := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "/flags/ui-flag/toggle", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Enabled {
		t.Fatalf("toggle result = %+v", resp)
	}
}
