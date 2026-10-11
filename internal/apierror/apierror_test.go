package apierror

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestWriteShape(t *testing.T) {
	rec := httptest.NewRecorder()

	Write(rec, 40490, Error{Code: 404, Status: "NOT_FOUND", Message: "not found", RequestID: "r1"})

	if rec.Code != 404 {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, rec.Body.String())
	}
	want := map[string]any{
		"isSuccess":  false,
		"systemCode": float64(40490),
		"error": map[string]any{
			"code":      float64(404),
			"status":    "NOT_FOUND",
			"message":   "not found",
			"requestId": "r1",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
}
