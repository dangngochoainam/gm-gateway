// Package apierror writes the single error shape the gateway exposes to
// clients. Upstream detail (stack traces, SQL, internal host names) stays in
// the log: whatever reaches this package is already safe to publish.
package apierror

import (
	"encoding/json"
	"net/http"
)

// FieldViolation names one request field the client must fix.
type FieldViolation struct {
	Field       string `json:"field"`
	Description string `json:"description"`
}

// Error is the error object returned by every gateway-generated error.
type Error struct {
	Code            int              `json:"code"`
	Status          string           `json:"status"`
	Message         string           `json:"message"`
	Reason          string           `json:"reason,omitempty"`
	RequestID       string           `json:"requestId,omitempty"`
	FieldViolations []FieldViolation `json:"fieldViolations,omitempty"`
}

// Write renders {"isSuccess": false, "systemCode": systemCode, "error": e} with
// HTTP status e.Code.
func Write(w http.ResponseWriter, systemCode int32, e Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.Code)
	_ = json.NewEncoder(w).Encode(struct {
		IsSuccess  bool  `json:"isSuccess"`
		SystemCode int32 `json:"systemCode"`
		Error      Error `json:"error"`
	}{false, systemCode, e})
}
