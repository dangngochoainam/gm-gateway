package gatewaymux

import "strings"

// jsonFieldPath rewrites a protovalidate field path to the JSON names clients see.
// Field names follow protojson's JSONName (drop '_', upper-case the next
// character); indexes and map keys inside [...] are copied verbatim.
func jsonFieldPath(protoPath string) string {
	var b strings.Builder
	inBrackets, upperNext := false, false
	for _, c := range protoPath {
		switch {
		case inBrackets:
			inBrackets = c != ']'
		case c == '[':
			inBrackets = true
		case c == '_':
			upperNext = true
			continue
		case upperNext && 'a' <= c && c <= 'z':
			c -= 'a' - 'A'
		}
		upperNext = false
		b.WriteRune(c)
	}
	return b.String()
}
