package main

import (
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
)

// docsHTML is a minimal Redoc viewer that loads the spec from /openapi.yaml.
// The bundle is fetched from the public Redoc CDN, so rendering requires
// internet access in the local dev environment.
const docsHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <title>Shorty API — Documentation</title>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link href="https://fonts.googleapis.com/css?family=Montserrat:300,400,700|Roboto:300,400,700" rel="stylesheet">
  <style>body { margin: 0; padding: 0; }</style>
</head>
<body>
  <redoc spec-url="/openapi.yaml"></redoc>
  <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
</body>
</html>`

// registerDocsHandlers wires up GET /docs and GET /openapi.yaml on the router.
// It is intended for LOCAL_MODE only: the spec is read from disk at request
// time (working directory must contain the spec file). In Lambda / production
// the spec is not shipped with the binary and these routes should not be
// registered.
func registerDocsHandlers(r chi.Router, specPath string) {
	r.Get("/docs", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(docsHTML))
	})

	r.Get("/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		data, err := os.ReadFile(specPath) //nolint:gosec // path is a server-side config, not user input
		if err != nil {
			http.Error(w, "openapi spec not found: "+err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}
