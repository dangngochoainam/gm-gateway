package gatewaymux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dangngochoainam/gopkg/commonhelper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// envelope decodes the top-level isSuccess and systemCode of a gateway body.
func envelope(t *testing.T, rec *httptest.ResponseRecorder) (isSuccess *bool, systemCode *int32) {
	t.Helper()
	var body struct {
		IsSuccess  *bool  `json:"isSuccess"`
		SystemCode *int32 `json:"systemCode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body.IsSuccess, body.SystemCode
}

func assertFailure(t *testing.T, rec *httptest.ResponseRecorder, wantCode int32) {
	t.Helper()
	isSuccess, systemCode := envelope(t, rec)
	if isSuccess == nil || *isSuccess {
		t.Errorf("isSuccess = %v, want false (%s)", isSuccess, rec.Body.String())
	}
	if systemCode == nil || *systemCode != wantCode {
		t.Errorf("systemCode = %v, want %d (%s)", systemCode, wantCode, rec.Body.String())
	}
}

func TestErrorHandlerSystemCode(t *testing.T) {
	tests := []struct {
		name string
		st   *status.Status
		want int32
	}{
		{"business reason", withDetails(t, codes.InvalidArgument, "bad request: x", &errdetails.ErrorInfo{Reason: "BAD_REQUEST", Domain: "gm-product"}), 40000},
		{"no details", status.New(codes.Unavailable, "connection refused"), 50300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, _ := handleError(t, tt.st)
			assertFailure(t, rec, tt.want)
		})
	}
}

func TestRoutingErrorHandlerSystemCode(t *testing.T) {
	for httpStatus, want := range map[int]int32{
		http.StatusNotFound:         40490,
		http.StatusMethodNotAllowed: 40590,
	} {
		ctx := context.WithValue(context.Background(), commonhelper.TRACE_ID, "r1")
		req := httptest.NewRequest(http.MethodGet, "/v1/khong-ton-tai", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		routingErrorHandler(ctx, nil, nil, rec, req, httpStatus)

		assertFailure(t, rec, want)
	}
}
