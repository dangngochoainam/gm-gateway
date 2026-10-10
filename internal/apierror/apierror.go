// Package apierror writes the single error shape the gateway exposes to
// clients. Upstream detail (stack traces, SQL, internal host names) stays in
// the log: whatever reaches this package is already safe to publish.
package apierror

import (
	"encoding/json"
	"net/http"
)

// Body is the error envelope returned by every gateway-generated error.
type Body struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// Write renders body as JSON with the given HTTP status.
func Write(w http.ResponseWriter, httpStatus int, body Body) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(body)
}
