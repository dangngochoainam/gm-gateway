package gatewaymux

import "testing"

func TestJSONFieldPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"limit", "limit"},
		{"sort_by", "sortBy"},
		{"items[0].unit_price", "items[0].unitPrice"},
		{`labels["some_key"].value_x`, `labels["some_key"].valueX`},
		{"a_b_c", "aBC"},
	}
	for _, tt := range tests {
		if got := jsonFieldPath(tt.in); got != tt.want {
			t.Errorf("jsonFieldPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
