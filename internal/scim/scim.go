// Package scim implements a minimal SCIM 2.0 provider (Users and Groups)
// over the store. This is the v1.0 procurement skeleton: schema shipped in
// every binary, endpoint mounted in serve behind a bearer token, and —
// fail-closed — an empty token disables the endpoint entirely. The cloud
// tier's IdP sync writes through the same store methods.
package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/switchyard/switchyard/internal/store"
)

// Server serves SCIM 2.0 resources over a Store.
type Server struct {
	st    *store.Store
	token string
}

// New builds a SCIM server. An empty token means the endpoint is disabled
// (every request 401s) — self-hosters opt in explicitly.
func New(st *store.Store, token string) *Server {
	return &Server{st: st, token: token}
}

// Handler wires the SCIM routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/scim/v2/ServiceProviderConfig", s.requireAuth(s.handleSPConfig))
	mux.HandleFunc("/scim/v2/Users", s.requireAuth(s.handleUsers))
	mux.HandleFunc("/scim/v2/Users/", s.requireAuth(s.handleUserPath))
	mux.HandleFunc("/scim/v2/Groups", s.requireAuth(s.handleGroups))
	mux.HandleFunc("/scim/v2/Groups/", s.requireAuth(s.handleGroupPath))
	return mux
}

// requireAuth enforces the bearer token. Fail-closed: no token configured,
// or any mismatch, means 401.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			writeSCIMError(w, http.StatusUnauthorized, "SCIM is disabled: no bearer token configured")
			return
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+s.token {
			writeSCIMError(w, http.StatusUnauthorized, "invalid bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleSPConfig(w http.ResponseWriter, _ *http.Request) {
	writeSCIM(w, http.StatusOK, map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://github.com/jonathanngiroux-star/switchyard",
		"patch":            map[string]any{"supported": false},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": false, "maxResults": 0},
		"changePassword":   map[string]any{"supported": false},
		"sort":             map[string]any{"supported": false},
		"etag":             map[string]any{"supported": false},
		"authSchemes": []map[string]any{{
			"type":        "oauthbearertoken",
			"name":        "Switchyard",
			"description": "Static bearer token (SWITCHYARD_SCIM_TOKEN)",
		}},
		"meta": map[string]any{"resourceType": "ServiceProviderConfig", "location": "/scim/v2/ServiceProviderConfig"},
	})
}

// --- Users ---

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := s.st.ListSCIMUsers(r.Context())
		if err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resources := []map[string]any{}
		for _, u := range users {
			resources = append(resources, scimUserFromStore(u))
		}
		writeSCIM(w, http.StatusOK, map[string]any{
			"schemas":      []string{"urn:ietf:params:scim:schemas:core:2.0:ListResponse"},
			"totalResults": len(resources),
			"Resources":    resources,
		})
	case http.MethodPost:
		var payload scimUserPayload
		if !decodeSCIM(w, r, &payload) {
			return
		}
		if payload.UserName == "" {
			writeSCIMError(w, http.StatusBadRequest, "userName is required")
			return
		}
		u := payload.toStore()
		if err := s.st.PutSCIMUser(r.Context(), u); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSCIM(w, http.StatusCreated, scimUserFromStore(u))
	default:
		writeSCIMError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUserPath(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/scim/v2/Users/")
	switch r.Method {
	case http.MethodGet:
		u, err := s.st.GetSCIMUser(r.Context(), id)
		if err == store.ErrNotFound {
			writeSCIMError(w, http.StatusNotFound, "user not found")
			return
		}
		if err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSCIM(w, http.StatusOK, scimUserFromStore(u))
	case http.MethodPut:
		var payload scimUserPayload
		if !decodeSCIM(w, r, &payload) {
			return
		}
		payload.ID = id
		u := payload.toStore()
		if err := s.st.PutSCIMUser(r.Context(), u); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSCIM(w, http.StatusOK, scimUserFromStore(u))
	case http.MethodDelete:
		if err := s.st.DeleteSCIMUser(r.Context(), id); err == store.ErrNotFound {
			writeSCIMError(w, http.StatusNotFound, "user not found")
			return
		} else if err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeSCIMError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type scimUserPayload struct {
	Schemas     []string        `json:"schemas"`
	ID          string          `json:"id"`
	UserName    string          `json:"userName"`
	DisplayName string          `json:"displayName"`
	Active      bool            `json:"active"`
	Emails      []scimEmail     `json:"emails"`
	Raw         json.RawMessage `json:"-"`
}

type scimEmail struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary"`
}

func (p scimUserPayload) toStore() store.SCIMUser {
	email := ""
	for _, e := range p.Emails {
		if e.Primary || email == "" {
			email = e.Value
		}
	}
	return store.SCIMUser{
		ID: p.ID, UserName: p.UserName, DisplayName: p.DisplayName,
		Email: email, Active: p.Active, Raw: string(p.Raw),
	}
}

func scimUserFromStore(u store.SCIMUser) map[string]any {
	out := map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:User"},
		"id":          u.ID,
		"userName":    u.UserName,
		"displayName": u.DisplayName,
		"active":      u.Active,
		"meta":        map[string]any{"resourceType": "User", "location": "/scim/v2/Users/" + u.ID},
	}
	if u.Email != "" {
		out["emails"] = []map[string]any{{"value": u.Email, "primary": true}}
	}
	return out
}

// --- Groups ---

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var payload scimGroupPayload
		if !decodeSCIM(w, r, &payload) {
			return
		}
		g := payload.toStore()
		if err := s.st.PutSCIMGroup(r.Context(), g); err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSCIM(w, http.StatusCreated, scimGroupFromStore(g))
	default:
		writeSCIMError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleGroupPath(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/scim/v2/Groups/")
	switch r.Method {
	case http.MethodGet:
		g, err := s.st.GetSCIMGroup(r.Context(), id)
		if err == store.ErrNotFound {
			writeSCIMError(w, http.StatusNotFound, "group not found")
			return
		}
		if err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeSCIM(w, http.StatusOK, scimGroupFromStore(g))
	case http.MethodDelete:
		if err := s.st.DeleteSCIMGroup(r.Context(), id); err == store.ErrNotFound {
			writeSCIMError(w, http.StatusNotFound, "group not found")
			return
		} else if err != nil {
			writeSCIMError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeSCIMError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

type scimGroupPayload struct {
	Schemas     []string     `json:"schemas"`
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	Members     []scimMember `json:"members"`
}

type scimMember struct {
	Value string `json:"value"`
}

func (p scimGroupPayload) toStore() store.SCIMGroup {
	ids := []string{}
	for _, m := range p.Members {
		ids = append(ids, m.Value)
	}
	return store.SCIMGroup{ID: p.ID, DisplayName: p.DisplayName, Members: ids, Raw: "{}"}
}

func scimGroupFromStore(g store.SCIMGroup) map[string]any {
	members := []map[string]any{}
	for _, id := range g.Members {
		members = append(members, map[string]any{"value": id})
	}
	return map[string]any{
		"schemas":     []string{"urn:ietf:params:scim:schemas:core:2.0:Group"},
		"id":          g.ID,
		"displayName": g.DisplayName,
		"members":     members,
		"meta":        map[string]any{"resourceType": "Group", "location": "/scim/v2/Groups/" + g.ID},
	}
}

func decodeSCIM(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeSCIMError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeSCIM(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeSCIMError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/scim+json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:Error"},
		"status":  code,
		"detail":  msg,
	})
}
