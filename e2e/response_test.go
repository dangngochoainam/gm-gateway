//go:build e2e

// Package e2e drives the running gateway over HTTP: gateway → gRPC → gm-product → DB.
package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func gatewayURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("E2E_GATEWAY_URL")
	if u == "" {
		t.Fatal("E2E_GATEWAY_URL is not set")
	}
	return strings.TrimRight(u, "/")
}

// do sends one request and returns the response with its body decoded as a
// JSON object.
func do(t *testing.T, method, path, body string, header map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, gatewayURL(t)+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s %s: body is not a JSON object: %v (%s)", method, path, err, raw)
	}
	return resp, decoded
}

func errorObject(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	e, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf(`body has no "error" object: %v`, body)
	}
	return e
}

func assertCode(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Errorf("HTTP status = %d, want %d", resp.StatusCode, want)
	}
}

func TestE2EListProductsSuccess(t *testing.T) {
	resp, body := do(t, http.MethodGet, "/v1/products?limit=2", "", nil)

	assertCode(t, resp, http.StatusOK)
	if resp.Header.Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing")
	}
	if body["isSuccess"] != true {
		t.Errorf("isSuccess = %v, want true (%v)", body["isSuccess"], body)
	}
	if body["systemCode"] != float64(1) {
		t.Errorf("systemCode = %v, want 1 (%v)", body["systemCode"], body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf(`body has no "data" object: %v`, body)
	}
	for _, k := range []string{"traceId", "statusCode", "reasonCode", "reasonMessage", "timeMs"} {
		if _, ok := body[k]; ok {
			t.Errorf("envelope key %q must be gone: %v", k, body)
		}
		if _, ok := data[k]; ok {
			t.Errorf("envelope key %q must be gone from data: %v", k, data)
		}
	}
	for _, k := range []string{"products", "pagination"} {
		if _, ok := data[k]; !ok {
			t.Errorf("key %q missing from data: %v", k, data)
		}
	}
}

func TestE2EListProductsInvalidSortBy(t *testing.T) {
	resp, body := do(t, http.MethodGet, "/v1/products?limit=2&sort_by=price", "",
		map[string]string{"X-Request-ID": "e2e-req-1"})

	assertCode(t, resp, http.StatusBadRequest)
	e := errorObject(t, body)
	if e["status"] != "INVALID_ARGUMENT" || e["reason"] != "BAD_REQUEST" {
		t.Errorf("error = %v, want status INVALID_ARGUMENT, reason BAD_REQUEST", e)
	}
	if msg, _ := e["message"].(string); !strings.Contains(msg, "sortBy") {
		t.Errorf("error.message = %q, want it to name sortBy", msg)
	}
	if e["requestId"] != "e2e-req-1" {
		t.Errorf("error.requestId = %v, want e2e-req-1", e["requestId"])
	}
}

func TestE2EListProductsValidation(t *testing.T) {
	resp, body := do(t, http.MethodGet, "/v1/products?limit=0", "", nil)

	assertCode(t, resp, http.StatusBadRequest)
	e := errorObject(t, body)
	if e["reason"] != "VALIDATION_FAILED" {
		t.Errorf("error.reason = %v, want VALIDATION_FAILED", e["reason"])
	}
	fv, _ := e["fieldViolations"].([]any)
	if len(fv) == 0 {
		t.Fatalf("error.fieldViolations missing: %v", e)
	}
	if first, _ := fv[0].(map[string]any); first["field"] != "limit" {
		t.Errorf("fieldViolations[0].field = %v, want limit", first["field"])
	}
}

func TestE2ECreateManyProductsEmpty(t *testing.T) {
	resp, body := do(t, http.MethodPost, "/v1/products/batch", `{"products":[]}`, nil)

	assertCode(t, resp, http.StatusBadRequest)
	e := errorObject(t, body)
	fv, _ := e["fieldViolations"].([]any)
	if len(fv) == 0 {
		t.Errorf("error.fieldViolations missing: %v", e)
	}
}

func TestE2EUnknownRoute(t *testing.T) {
	resp, body := do(t, http.MethodGet, "/v1/khong-ton-tai", "", nil)

	assertCode(t, resp, http.StatusNotFound)
	if e := errorObject(t, body); e["status"] != "NOT_FOUND" {
		t.Errorf("error.status = %v, want NOT_FOUND", e["status"])
	}
}
