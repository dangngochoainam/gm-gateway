package apierror

import "testing"

func TestSystemCode(t *testing.T) {
	tests := []struct {
		reason     string
		httpStatus int
		want       int32
	}{
		{"VALIDATION_FAILED", 400, 40001},
		{"BAD_REQUEST", 400, 40000},
		{"", 503, 50300},
		{"PRODUCT_GONE", 404, 40400},
		{"SYSTEM_CODE_UNSPECIFIED", 400, 40000},
	}
	for _, tt := range tests {
		if got := SystemCode(tt.reason, tt.httpStatus); got != tt.want {
			t.Errorf("SystemCode(%q, %d) = %d, want %d", tt.reason, tt.httpStatus, got, tt.want)
		}
	}
}
