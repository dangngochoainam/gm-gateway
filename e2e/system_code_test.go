//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// assertEnvelope checks the top-level isSuccess and systemCode of a body.
func assertEnvelope(t *testing.T, body map[string]any, wantSuccess bool, wantCode float64) {
	t.Helper()
	if body["isSuccess"] != wantSuccess {
		t.Errorf("isSuccess = %v, want %v (%v)", body["isSuccess"], wantSuccess, body)
	}
	if body["systemCode"] != wantCode {
		t.Errorf("systemCode = %v, want %v (%v)", body["systemCode"], wantCode, body)
	}
}

func TestE2ECreateManyProductsSuccess(t *testing.T) {
	code := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	resp, body := do(t, http.MethodPost, "/v1/products/batch",
		fmt.Sprintf(`{"products":[{"name":"E2E Product","code":%q,"price":1}]}`, code), nil)

	assertCode(t, resp, http.StatusOK)
	assertEnvelope(t, body, true, 1)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf(`body has no "data" object: %v`, body)
	}
	if products, _ := data["products"].([]any); len(products) != 1 {
		t.Errorf("data.products = %v, want 1 product", data["products"])
	}
}

func TestE2ESystemCodes(t *testing.T) {
	tests := []struct {
		path       string
		httpStatus int
		systemCode float64
	}{
		{"/v1/products?limit=2&sort_by=price", http.StatusBadRequest, 40000},
		{"/v1/products?limit=0", http.StatusBadRequest, 40001},
		{"/v1/khong-ton-tai", http.StatusNotFound, 40490},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, body := do(t, http.MethodGet, tt.path, "", nil)

			assertCode(t, resp, tt.httpStatus)
			assertEnvelope(t, body, false, tt.systemCode)
		})
	}
}
