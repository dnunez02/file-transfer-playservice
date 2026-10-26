// Package health serves the HTTP health check a load balancer polls over TCP.
package health

import (
	"log"
	"net/http"
	"sync/atomic"
)

// Handler builds the handler for GET /health, which a load balancer polls over TCP
// because it cannot health-check a UDP port.
//
// Inputs: draining is set when the server is shutting down.
// Returns: a handler that replies 200 {"status":"ok"} normally and 503 {"status":"draining"}
// while draining, so the load balancer stops sending new requests.
// Side effects: none.
func Handler(draining *atomic.Bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"draining"}`))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// Serve runs the health check HTTP server on address until the program exits.
//
// Errors: ends the program through log.Fatal when it cannot listen on address.
// Side effects: listens on a TCP port.
func Serve(address string, draining *atomic.Bool) {
	log.Fatal(http.ListenAndServe(address, Handler(draining)))
}
