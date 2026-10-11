package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func assertFailure(t *testing.T, rec *httptest.ResponseRecorder, wantCode int32) {
	t.Helper()
	var body struct {
		IsSuccess  *bool  `json:"isSuccess"`
		SystemCode *int32 `json:"systemCode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.IsSuccess == nil || *body.IsSuccess {
		t.Errorf("isSuccess = %v, want false (%s)", body.IsSuccess, rec.Body.String())
	}
	if body.SystemCode == nil || *body.SystemCode != wantCode {
		t.Errorf("systemCode = %v, want %d (%s)", body.SystemCode, wantCode, rec.Body.String())
	}
}

func TestRecoverySystemCode(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), RequestID, Recovery)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/products", nil))

	assertFailure(t, rec, 50090)
}

func TestMaxBodyBytesSystemCode(t *testing.T) {
	handler := MaxBodyBytes(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/say-hello", strings.NewReader(strings.Repeat("a", 64))))

	assertFailure(t, rec, 41390)
}
