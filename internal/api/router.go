package api

import (
	"encoding/json"
	"net/http"

	apigen "robottt/internal/api/gen"
)

// maxBodyBytes bounds request body size; these are small fixed-shape JSON
// bodies, anything larger is malformed or abusive. The generated strict
// handler doesn't apply a body-size limit itself, so it's done here as
// middleware instead.
const maxBodyBytes = 1024

// Middleware wraps an http.Handler. Auth (spec §7) slots in later as an
// entry in the mw list passed to NewRouter, no other change required.
type Middleware func(http.Handler) http.Handler

func chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// NewRouter builds the HTTP handler for all robot control endpoints from
// the generated strict server, wired to h, plus a GET /openapi.yaml route
// serving the contract itself (so an MCP wrapper or other client can fetch
// it at runtime instead of needing repo access).
func NewRouter(h *Handlers, mw ...Middleware) http.Handler {
	strict := apigen.NewStrictHandlerWithOptions(h, nil, apigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeStrictError,
		ResponseErrorHandlerFunc: writeStrictError,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /openapi.yaml", serveOpenAPISpec)
	handler := apigen.HandlerFromMux(strict, mux)

	return chain(handler, append([]Middleware{limitBody}, mw...)...)
}

// writeStrictError renders request-decode/response-encode failures from the
// generated server in the same {"error": "..."} shape as validation errors,
// instead of the generator's default plain-text body.
func writeStrictError(w http.ResponseWriter, _ *http.Request, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
