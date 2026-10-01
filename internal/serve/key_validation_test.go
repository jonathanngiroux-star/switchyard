package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

// validKey mirrors the embedded UI's input pattern: the flag key
// alphabet is [a-zA-Z0-9._-]+. The API must enforce the same contract
// server-side — client-side patterns are not a boundary. Hostile keys
// (HTML metacharacters) previously round-tripped through the store and
// into the UI's innerHTML: stored XSS in the operator console.
func TestCreateFlagRejectsInvalidKey(t *testing.T) {
	srv, _ := newServer(t)
	h := srv.Handler()
	for _, key := range []string{
		"", "<img src=x onerror=alert(1)>", "x\" onmouseover=\"alert(2)",
		"flag with spaces", "flag/../../etc", "café",
	} {
		body := `{"key":"` + strings.ReplaceAll(strings.ReplaceAll(key, `\`, `\\`), `"`, `\"`) + `","kind":"boolean"}`
		r := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("key %q: code = %d %s, want 400", key, w.Code, w.Body.String())
		}
	}
}

func TestCreateFlagAcceptsValidKeys(t *testing.T) {
	srv, _ := newServer(t)
	h := srv.Handler()
	for _, key := range []string{"a", "checkout-v2", "flag.key", "Flag_1", "A-z9_."} {
		body := `{"key":"` + key + `","kind":"boolean"}`
		r := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Errorf("key %q: code = %d %s, want 201", key, w.Code, w.Body.String())
		}
	}
}

// Migration is the product; imported flag keys must keep working.
func TestImportedKeysStillRoundTripAfterValidation(t *testing.T) {
	srv, _ := newServer(t)
	h := srv.Handler()
	// Keys drawn from the real fixture corpora.
	for _, key := range []string{"checkouts-v2", "api-rate-limit", "feature-a", "flag.with.dots"} {
		body := `{"key":"` + key + `","kind":"boolean"}`
		r := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Errorf("imported-style key %q: code = %d, want 201", key, w.Code)
		}
	}
}

// PUT must validate the same way — it can create via upsert.
func TestUpdateFlagRejectsInvalidKey(t *testing.T) {
	srv, _ := newServer(t)
	// Create one valid flag first so the 400 is about the key, not 404.
	srq := httptest.NewRequest(http.MethodPost, "/flags", strings.NewReader(`{"key":"ok-flag","kind":"boolean"}`))
	srq.Header.Set("Content-Type", "application/json")
	sw := httptest.NewRecorder()
	srv.Handler().ServeHTTP(sw, srq)
	if sw.Code != http.StatusCreated {
		t.Fatalf("seed create = %d", sw.Code)
	}
	h := srv.Handler()
	r := httptest.NewRequest(http.MethodPut, "/flags/%3Cscript%3E", strings.NewReader(`{"kind":"boolean"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT hostile path key: code = %d %s, want 400", w.Code, w.Body.String())
	}
}

var _ = store.ErrNotFound
