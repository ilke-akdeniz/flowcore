package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// The built front end, compiled into the binary.
//
// This is what keeps the console runnable in two commands: `docker compose up`
// and `go run .` serve the API and the application from one process, with no
// separate web server and nothing to deploy alongside. Vite writes web/dist;
// `make build` runs it before the Go build.
//
//go:embed all:web/dist
var assetFS embed.FS

// assets serves the built application, falling back to the shell for any path it
// does not recognise.
//
// The fallback is what makes client-side routing work: React Router owns /cases
// and /workflows, so a browser asked to load one directly must still receive
// index.html rather than a 404.
func assets() (http.Handler, error) {
	files, err := fs.Sub(assetFS, "web/dist")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(files))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A request with an extension is a real asset; anything else is a route.
		if path := strings.TrimPrefix(r.URL.Path, "/"); path != "" && !strings.Contains(path, ".") {
			if _, err := fs.Stat(files, path); err != nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		}

		fileServer.ServeHTTP(w, r)
	}), nil
}
