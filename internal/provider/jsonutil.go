package provider

import (
	"bytes"
	"encoding/json"
)

// jsonSubset reports whether every value in want is present and equal in got. Objects may have
// extra keys in got (server defaults); arrays must match in length and order.
func jsonSubset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok || !jsonSubset(wv, gv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !jsonSubset(w[i], g[i]) {
				return false
			}
		}
		return true
	default:
		return want == got
	}
}

// jsonCovers reports whether the JSON text in got contains everything in want (both must parse).
func jsonCovers(want string, got []byte) bool {
	var w, g any
	if json.Unmarshal([]byte(want), &w) != nil || json.Unmarshal(got, &g) != nil {
		return false
	}
	return jsonSubset(w, g)
}

// compactJSON re-encodes raw JSON without whitespace.
func compactJSON(raw []byte) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}
