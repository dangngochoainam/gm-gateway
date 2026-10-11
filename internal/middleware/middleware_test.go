package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dangngochoainam/gopkg/loghelper"
)

func TestMain(m *testing.M) {
	// Recovery and AccessLog log through the package-level zap logger.
	if err := loghelper.InitZapLogger("prod", "middleware-test"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestStripIdentityHeadersDropsClientSpoofing(t *testing.T) {
	var forwarded http.Header
	handler := StripIdentityHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		forwarded = r.Header.Clone()
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	req.Header.Set("X-User-ID", "victim")
	req.Header.Set("x-roles", "admin")
	req.Header.Set("X-Internal-Service", "gm-core")
	req.Header.Set("Authorization", "Bearer token")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	for _, name := range []string{"X-User-ID", "X-Roles", "X-Internal-Service"} {
		if got := forwarded.Get(name); got != "" {
			t.Errorf("header %s should have been stripped, got %q", name, got)
		}
	}
	if forwarded.Get("Authorization") != "Bearer token" {
		t.Error("Authorization must survive stripping")
	}
}

func TestRequestIDReusesClientValue(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/products", nil)
	req.Header.Set(RequestIDHeader, "from-client")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if seen != "from-client" {
		t.Errorf("context request id = %q, want from-client", seen)
	}
	if rec.Header().Get(RequestIDHeader) != "from-client" {
		t.Error("request id must be echoed on the response")
	}
}

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/products", nil))

	if seen == "" {
		t.Error("a request id must be generated when the client sends none")
	}
}

func TestMaxBodyBytesRejectsDeclaredOversize(t *testing.T) {
	called := false
	handler := MaxBodyBytes(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/say-hello", strings.NewReader(strings.Repeat("a", 64)))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if called {
		t.Error("an oversized body must not reach the next handler")
	}
}

func TestRecoveryConvertsPanicTo500(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), RequestID, Recovery)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/products", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Error("panic detail must not reach the client")
	}
}

// errorStatus decodes the "error.status" field of a gateway error body.
func errorStatus(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Status
}

func TestRecoveryWritesErrorShape(t *testing.T) {
	handler := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}), RequestID, Recovery)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/products", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := errorStatus(t, rec); got != "INTERNAL" {
		t.Errorf("error.status = %q, want INTERNAL", got)
	}
}

func TestMaxBodyBytesWritesErrorShape(t *testing.T) {
	handler := MaxBodyBytes(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/say-hello", strings.NewReader(strings.Repeat("a", 64))))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
	if got := errorStatus(t, rec); got != "REQUEST_TOO_LARGE" {
		t.Errorf("error.status = %q, want REQUEST_TOO_LARGE", got)
	}
}

func TestMaxBodyBytesAllowsExactLimit(t *testing.T) {
	called := false
	handler := MaxBodyBytes(16)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/say-hello", strings.NewReader(strings.Repeat("a", 16))))

	if !called {
		t.Error("request at exactly the limit must reach the next handler")
	}
	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Error("request at exactly the limit must not get 413")
	}
}
