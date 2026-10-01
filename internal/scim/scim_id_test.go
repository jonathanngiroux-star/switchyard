package scim

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// RFC 7644 §3.3: the server MUST assign the id on create. Real IdPs
// (Okta, Entra) POST users without an id; Switchyard used to store
// them under id=” — every such user overwrote the previous one and
// the 201 carried "id":"" which no client can address.
func TestCreateUserWithoutIDAssignsServerID(t *testing.T) {
	s, _ := newTestServer(t)
	// No "id" in the payload — exactly what Okta sends.
	body := userPayload("", "alice@corp.test", "Alice", true)
	delete(body, "id")
	rr := doSCIM(t, s, "POST", "/scim/v2/Users", "test-token", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("server must assign a non-empty id, got %q (body: %s)", created.ID, rr.Body.String())
	}
	// The assigned id must address the resource.
	rr = doSCIM(t, s, "GET", "/scim/v2/Users/"+created.ID, "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get by assigned id = %d, want 200 — id must address the resource", rr.Code)
	}
}

func TestTwoUsersWithoutIDDoNotCollide(t *testing.T) {
	s, _ := newTestServer(t)
	ids := map[string]bool{}
	for _, name := range []string{"alice@corp.test", "bob@corp.test"} {
		body := userPayload("", name, name, true)
		delete(body, "id")
		rr := doSCIM(t, s, "POST", "/scim/v2/Users", "test-token", body)
		if rr.Code != http.StatusCreated {
			t.Fatalf("create %s = %d: %s", name, rr.Code, rr.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		json.Unmarshal(rr.Body.Bytes(), &created)
		if created.ID == "" {
			t.Fatalf("create %s: empty id", name)
		}
		ids[created.ID] = true
	}
	if len(ids) != 2 {
		t.Fatalf("two creates without id must produce two distinct ids, got %v", ids)
	}
	// List must show both users — not one overwritten by the other.
	rr := doSCIM(t, s, "GET", "/scim/v2/Users", "test-token", nil)
	for _, name := range []string{"alice@corp.test", "bob@corp.test"} {
		if !strings.Contains(rr.Body.String(), name) {
			t.Fatalf("list missing %s — data was overwritten by the empty-id collision", name)
		}
	}
}

// PUT (replace) must keep the id from the URL, not from the body — and
// must not be able to move a user onto the empty id.
func TestPutUserKeepsPathID(t *testing.T) {
	s, _ := newTestServer(t)
	created := userPayload("", "carol@corp.test", "Carol", true)
	delete(created, "id")
	rr := doSCIM(t, s, "POST", "/scim/v2/Users", "test-token", created)
	var c struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &c)
	if c.ID == "" {
		t.Fatalf("setup: no assigned id: %s", rr.Body.String())
	}
	// PUT without id in body — id must stay the path id.
	put := userPayload("", "carol@corp.test", "Carol II", false)
	delete(put, "id")
	rr = doSCIM(t, s, "PUT", "/scim/v2/Users/"+c.ID, "test-token", put)
	if rr.Code != http.StatusOK {
		t.Fatalf("put = %d: %s", rr.Code, rr.Body.String())
	}
	var updated struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
	}
	json.Unmarshal(rr.Body.Bytes(), &updated)
	if updated.ID != c.ID {
		t.Fatalf("put changed id: %q -> %q", c.ID, updated.ID)
	}
	rr = doSCIM(t, s, "GET", "/scim/v2/Users/"+c.ID, "test-token", nil)
	if !strings.Contains(rr.Body.String(), "Carol II") {
		t.Fatalf("put did not persist: %s", rr.Body.String())
	}
}

// Groups follow the same server-assigned-id contract.
func TestCreateGroupWithoutIDAssignsServerID(t *testing.T) {
	s, _ := newTestServer(t)
	group := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"displayName": "Platform team",
	}
	rr := doSCIM(t, s, "POST", "/scim/v2/Groups", "test-token", group)
	if rr.Code != http.StatusCreated {
		t.Fatalf("group create = %d: %s", rr.Code, rr.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rr.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatalf("server must assign group id, got %q", created.ID)
	}
	rr = doSCIM(t, s, "GET", "/scim/v2/Groups/"+created.ID, "test-token", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get group by assigned id = %d", rr.Code)
	}
}
