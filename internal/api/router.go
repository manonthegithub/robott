package api

import "net/http"

// Middleware wraps an http.Handler. Auth (spec §7) slots in later as an
// entry in the mw list passed to NewRouter, no other change required.
type Middleware func(http.Handler) http.Handler

func chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// NewRouter builds the HTTP handler for all robot control endpoints.
func NewRouter(h *Handlers, mw ...Middleware) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /led", h.HandleLED)
	mux.HandleFunc("POST /stepper", h.HandleStepper)
	mux.HandleFunc("POST /servo", h.HandleServo)
	return chain(mux, mw...)
}
