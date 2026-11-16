package provider

import "testing"

func TestJSONCovers(t *testing.T) {
	api := []byte(`[{"core":{"name":"title","guid":"g1","required":true},"min_length":0,"extra":1}]`)
	cases := []struct {
		want string
		ok   bool
	}{
		{`[{"core":{"name":"title"},"min_length":0}]`, true},
		{`[{"core":{"name":"title","required":true}}]`, true},
		{`[{"core":{"name":"other"}}]`, false}, // value differs
		{`[{"core":{"missing":1}}]`, false},    // key absent in API
		{`[]`, false},                          // length differs
		{`[{"core":{}},{"core":{}}]`, false},   // extra element
		{`not json`, false},
	}
	for _, c := range cases {
		if got := jsonCovers(c.want, api); got != c.ok {
			t.Errorf("jsonCovers(%s) = %v, want %v", c.want, got, c.ok)
		}
	}
}

func TestCompactJSON(t *testing.T) {
	if got := compactJSON([]byte("[ { \"a\" : 1 } ]")); got != `[{"a":1}]` {
		t.Errorf("got %s", got)
	}
}

func TestSettingsSectionDefaults(t *testing.T) {
	// compactJSON is what the data source applies to each section it receives.
	if got := compactJSON([]byte("{ \"currency\" : [ ] }")); got != `{"currency":[]}` {
		t.Errorf("got %s", got)
	}
}
