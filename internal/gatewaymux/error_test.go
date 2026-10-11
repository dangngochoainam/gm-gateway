package gatewaymux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dangngochoainam/gopkg/commonhelper"
	"github.com/dangngochoainam/gopkg/loghelper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
)

func TestMain(m *testing.M) {
	// errorHandler and routingErrorHandler log through the package-level zap logger.
	if err := loghelper.InitZapLogger("prod", "gatewaymux-test"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// handleError runs errorHandler for st and returns the recorder and the
// decoded "error" object of the body.
func handleError(t *testing.T, st *status.Status) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	ctx := context.WithValue(context.Background(), commonhelper.TRACE_ID, "r1")
	req := httptest.NewRequest(http.MethodGet, "/v1/products", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	errorHandler(ctx, nil, nil, rec, req, st.Err())

	return rec, decodeError(t, rec)
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Error == nil {
		t.Fatalf(`body has no "error" object: %s`, rec.Body.String())
	}
	return body.Error
}

func withDetails(t *testing.T, code codes.Code, msg string, details ...protoadapt.MessageV1) *status.Status {
	t.Helper()
	st, err := status.New(code, msg).WithDetails(details...)
	if err != nil {
		t.Fatalf("WithDetails: %v", err)
	}
	return st
}

func assertField(t *testing.T, e map[string]any, key string, want any) {
	t.Helper()
	if got := e[key]; !reflect.DeepEqual(got, want) {
		t.Errorf("error.%s = %v, want %v", key, got, want)
	}
}

func TestErrorHandlerBusinessError(t *testing.T) {
	st := withDetails(t, codes.InvalidArgument, "bad request: sortBy must be created_at or name",
		&errdetails.ErrorInfo{Reason: "BAD_REQUEST", Domain: "gm-product"})

	rec, e := handleError(t, st)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	assertField(t, e, "code", float64(400))
	assertField(t, e, "status", "INVALID_ARGUMENT")
	assertField(t, e, "message", "bad request: sortBy must be created_at or name")
	assertField(t, e, "reason", "BAD_REQUEST")
	assertField(t, e, "requestId", "r1")
	if strings.Contains(rec.Body.String(), "gm-product") {
		t.Errorf("ErrorInfo.domain must not reach the client: %s", rec.Body.String())
	}
}

func TestErrorHandlerValidation(t *testing.T) {
	st := withDetails(t, codes.InvalidArgument, "validation error: items[0].unit_price: d",
		&errdetails.ErrorInfo{Reason: "VALIDATION_FAILED", Domain: "gm-product"},
		&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "items[0].unit_price", Description: "d"},
		}})

	_, e := handleError(t, st)

	assertField(t, e, "reason", "VALIDATION_FAILED")
	assertField(t, e, "fieldViolations", []any{
		map[string]any{"field": "items[0].unitPrice", "description": "d"},
	})
}

func TestErrorHandlerHidesDetailsOn5xx(t *testing.T) {
	st := withDetails(t, codes.Internal, "pq: password auth failed",
		&errdetails.ErrorInfo{Reason: "INTERNAL_SERVER_ERROR", Domain: "gm-product"},
		&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "limit", Description: "d"},
		}})

	rec, e := handleError(t, st)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	assertField(t, e, "message", "Internal server error")
	assertField(t, e, "reason", "INTERNAL_SERVER_ERROR")
	if _, ok := e["fieldViolations"]; ok {
		t.Errorf("fieldViolations must not be published on 5xx: %v", e)
	}
	if strings.Contains(rec.Body.String(), "pq:") {
		t.Errorf("upstream detail leaked: %s", rec.Body.String())
	}
}

func TestErrorHandlerUpstreamUnavailable(t *testing.T) {
	rec, e := handleError(t, status.New(codes.Unavailable, "connection refused"))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
	assertField(t, e, "status", "UNAVAILABLE")
	assertField(t, e, "message", "Service temporarily unavailable")
	if _, ok := e["reason"]; ok {
		t.Errorf("reason must be absent without ErrorInfo: %v", e)
	}
}

func TestRoutingErrorHandler(t *testing.T) {
	tests := []struct {
		httpStatus int
		status     string
		message    string
	}{
		{http.StatusNotFound, "NOT_FOUND", "Not Found"},
		{http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method Not Allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), commonhelper.TRACE_ID, "r1")
			req := httptest.NewRequest(http.MethodGet, "/v1/khong-ton-tai", nil).WithContext(ctx)
			rec := httptest.NewRecorder()

			routingErrorHandler(ctx, nil, nil, rec, req, tt.httpStatus)

			if rec.Code != tt.httpStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.httpStatus)
			}
			e := decodeError(t, rec)
			assertField(t, e, "code", float64(tt.httpStatus))
			assertField(t, e, "status", tt.status)
			assertField(t, e, "message", tt.message)
			assertField(t, e, "requestId", "r1")
		})
	}
}

func TestErrorHandlerKeepsAllViolations(t *testing.T) {
	st := withDetails(t, codes.InvalidArgument, "validation error",
		&errdetails.ErrorInfo{Reason: "VALIDATION_FAILED", Domain: "gm-product"},
		&errdetails.BadRequest{FieldViolations: []*errdetails.BadRequest_FieldViolation{
			{Field: "sort_by", Description: "d1"},
			{Field: "items[1].unit_price", Description: "d2"},
		}})

	_, e := handleError(t, st)

	assertField(t, e, "fieldViolations", []any{
		map[string]any{"field": "sortBy", "description": "d1"},
		map[string]any{"field": "items[1].unitPrice", "description": "d2"},
	})
}

func TestErrorHandlerMinimalBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/products/p1", nil)
	rec := httptest.NewRecorder()

	errorHandler(context.Background(), nil, nil, rec, req, status.Error(codes.NotFound, "product not found"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	e := decodeError(t, rec)
	assertField(t, e, "message", "product not found")
	for _, key := range []string{"requestId", "reason", "fieldViolations"} {
		if _, ok := e[key]; ok {
			t.Errorf("key %q must be omitted when empty: %v", key, e)
		}
	}
}

func TestErrorHandlerUpstreamTimeout(t *testing.T) {
	st := withDetails(t, codes.DeadlineExceeded, "context deadline exceeded",
		&errdetails.ErrorInfo{Reason: "DEADLINE_EXCEEDED", Domain: "gm-product"})

	rec, e := handleError(t, st)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", rec.Code)
	}
	assertField(t, e, "status", "DEADLINE_EXCEEDED")
	assertField(t, e, "message", "Upstream timeout")
	assertField(t, e, "reason", "DEADLINE_EXCEEDED")
}
