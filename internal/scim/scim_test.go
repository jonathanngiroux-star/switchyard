package scim

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/switchyard/switchyard/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "scim.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return New(st, "test-token"), st
}

func doSCIM(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/scim+json")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	return w
}

func userPayload(id, userName, displayName string, active bool) map[string]any {
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"id":          id,
		"userName":    userName,
		"displayName": displayName,
		"active":      active,
		"emails":      []map[string]any{{"value": userName, "primary": true}},
	}
}

func TestMissingTokenIs401(t *testing.T) {
	s, _ := newTestServer(t)
	if code := doSCIM(t, s, "GET", "/scim/v2/Users", "", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", code)
	}
}

func TestWrongTokenIs401(t *testing.T) {
	s, _ := newTestServer(t)
	if code := doSCIM(t, s, "GET", "/scim/v2/Users", "wrong", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", code)
	}
}

func TestEmptyTokenConfigDisablesSCIM(t *testing.T) {
	// Fail-closed: no token configured = endpoint refuses everything.
	st, err := store.Open(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	s := New(st, "")
	if code := doSCIM(t, s, "GET", "/scim/v2/Users", "", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("no configured token = %d, want 401 (fail closed)", code)
	}
}

func TestUserCRUDRoundTrip(t *testing.T) {
	s, _ := newTestServer(t)
	// Create
	if code := doSCIM(t, s, "POST", "/scim/v2/Users", "test-token",
		userPayload("usr-1", "alice@corp.test", "Alice", true)).Code; code != http.StatusCreated {
		t.Fatalf("create = %d", code)
	}
	// Read
	rr := doSCIM(t, s, "GET", "/scim/v2/Users/usr-1", "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", rr.Code, rr.Body.String())
	}
	var u struct {
		ID          string `json:"id"`
		UserName    string `json:"userName"`
		DisplayName string `json:"displayName"`
		Active      *bool  `json:"active"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &u); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if u.ID != "usr-1" || u.UserName != "alice@corp.test" || u.Active == nil || !*u.Active {
		t.Fatalf("user = %+v", u)
	}
	// List
	rr = doSCIM(t, s, "GET", "/scim/v2/Users", "test-token", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "alice@corp.test") {
		t.Fatalf("list = %d %s", rr.Code, rr.Body.String())
	}
	// Update (deactivate)
	payload := userPayload("usr-1", "alice@corp.test", "Alice", false)
	if code := doSCIM(t, s, "PUT", "/scim/v2/Users/usr-1", "test-token", payload).Code; code != http.StatusOK {
		t.Fatalf("put = %d", code)
	}
	rr = doSCIM(t, s, "GET", "/scim/v2/Users/usr-1", "test-token", nil)
	var u2 struct {
		Active *bool `json:"active"`
	}
	json.Unmarshal(rr.Body.Bytes(), &u2)
	if u2.Active == nil || *u2.Active {
		t.Fatalf("after deactivation active = %+v", u2.Active)
	}
	// Delete
	if code := doSCIM(t, s, "DELETE", "/scim/v2/Users/usr-1", "test-token", nil).Code; code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", code)
	}
	if code := doSCIM(t, s, "GET", "/scim/v2/Users/usr-1", "test-token", nil).Code; code != http.StatusNotFound {
		t.Fatalf("get deleted = %d, want 404", code)
	}
}

func TestGroupCRUDRoundTrip(t *testing.T) {
	s, _ := newTestServer(t)
	// Create user first
	doSCIM(t, s, "POST", "/scim/v2/Users", "test-token", userPayload("usr-1", "bob@corp.test", "Bob", true))
	group := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"id":          "grp-1",
		"displayName": "Platform team",
		"members":     []map[string]any{{"value": "usr-1", "display": "Bob"}},
	}
	if code := doSCIM(t, s, "POST", "/scim/v2/Groups", "test-token", group).Code; code != http.StatusCreated {
		t.Fatalf("group create = %d", code)
	}
	rr := doSCIM(t, s, "GET", "/scim/v2/Groups/grp-1", "test-token", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "usr-1") {
		t.Fatalf("group get = %d %s", rr.Code, rr.Body.String())
	}
	if code := doSCIM(t, s, "DELETE", "/scim/v2/Groups/grp-1", "test-token", nil).Code; code != http.StatusNoContent {
		t.Fatalf("group delete = %d, want 204", code)
	}
}

func TestServiceProviderConfig(t *testing.T) {
	s, _ := newTestServer(t)
	rr := doSCIM(t, s, "GET", "/scim/v2/ServiceProviderConfig", "test-token", nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Switchyard") {
		t.Fatalf("spconfig = %d %s", rr.Code, rr.Body.String())
	}
}
