package serve

// ui.go serves the embedded control-plane UI (internal/serve/ui.go.html)
// and conditionally mounts the SCIM endpoint. The UI is deliberately one
// static page over the JSON API — no framework, no build step, no CDN
// fetches. Ugly is fine; the evaluator and the migrator are the product.

import (
	"embed"
	"net/http"
	"os"

	"github.com/switchyard/switchyard/internal/scim"
	"github.com/switchyard/switchyard/internal/store"
)

//go:embed ui.go.html
var uiFS embed.FS

// registerExtras mounts the UI at / and, when SWITCHYARD_SCIM_TOKEN is
// set to a non-empty value, the SCIM 2.0 endpoint. Fail-closed: no token,
// no SCIM surface.
func (s *Server) registerExtras(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		data, err := uiFS.ReadFile("ui.go.html")
		if err != nil {
			http.Error(w, "ui unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	if token := os.Getenv("SWITCHYARD_SCIM_TOKEN"); token != "" {
		scimSrv := scim.New(s.st, token)
		mux.Handle("/scim/", scimSrv.Handler())
	} else {
		// Fail-closed: without a token the SCIM surface does not exist.
		mux.HandleFunc("/scim/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"scim disabled: set SWITCHYARD_SCIM_TOKEN to enable"}`, http.StatusNotFound)
		})
	}
}

var _ = store.ErrNotFound // keep the store import until v1.0 slices land
