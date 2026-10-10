// Package health serves the two probes Kubernetes needs: liveness says the
// process is alive, readiness says it may receive traffic.
package health

import (
	"net/http"
	"sync/atomic"
)

// Readiness is flipped on once routes are registered and flipped off the moment
// SIGTERM arrives, so the pod leaves the Service endpoint list before the
// server starts refusing connections.
type Readiness struct {
	ready atomic.Bool
}

func NewReadiness() *Readiness {
	return &Readiness{}
}

func (r *Readiness) SetReady() {
	r.ready.Store(true)
}

func (r *Readiness) SetNotReady() {
	r.ready.Store(false)
}

// Register mounts /healthz and /readyz. They sit outside the middleware chain:
// probes run every few seconds and would otherwise flood the access log.
func Register(mux *http.ServeMux, readiness *Readiness) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, "ok")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !readiness.ready.Load() {
			writePlain(w, http.StatusServiceUnavailable, "not ready")
			return
		}
		writePlain(w, http.StatusOK, "ready")
	})
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
