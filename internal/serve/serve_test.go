package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func doPost(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestHealthz(t *testing.T) {
	rr := doGet(t, New().Handler(), "/healthz")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestListFlagsSeededWithWelcomeBanner(t *testing.T) {
	rr := doGet(t, New().Handler(), "/flags")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	var body struct {
		Flags []struct {
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
		} `json:"flags"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Flags) != 1 || body.Flags[0].Key != "welcome-banner" || body.Flags[0].Enabled {
		t.Fatalf("flags = %+v, want welcome-banner disabled", body.Flags)
	}
}

func TestToggleFlagFlipsState(t *testing.T) {
	h := New().Handler()
	rr := doPost(t, h, "/flags/welcome-banner/toggle")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	var toggled struct {
		Key     string `json:"key"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &toggled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !toggled.Enabled {
		t.Fatalf("first toggle should enable, got %+v", toggled)
	}
	rr = doPost(t, h, "/flags/welcome-banner/toggle")
	if err := json.Unmarshal(rr.Body.Bytes(), &toggled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if toggled.Enabled {
		t.Fatalf("second toggle should disable, got %+v", toggled)
	}
}

func TestToggleUnknownFlagReturns404(t *testing.T) {
	rr := doPost(t, New().Handler(), "/flags/nope/toggle")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}
