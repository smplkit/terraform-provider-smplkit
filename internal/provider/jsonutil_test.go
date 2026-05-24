package provider

import (
	"testing"
)

func TestCanonicalJSON_SortsKeys(t *testing.T) {
	got, err := canonicalJSON(`{"b":1,"a":[3,2,1]}`)
	if err != nil {
		t.Fatalf("canonicalJSON returned err: %v", err)
	}
	want := `{"a":[3,2,1],"b":1}`
	if got != want {
		t.Errorf("canonicalJSON sort: got %q want %q", got, want)
	}
}

func TestCanonicalJSON_EmptyInput(t *testing.T) {
	got, err := canonicalJSON("")
	if err != nil {
		t.Fatalf("canonicalJSON returned err on empty: %v", err)
	}
	if got != "" {
		t.Errorf("canonicalJSON empty: got %q want \"\"", got)
	}
}

func TestParseJSONString_Null(t *testing.T) {
	v, err := parseJSONString("null")
	if err != nil {
		t.Fatalf("parseJSONString(null) err: %v", err)
	}
	if v != nil {
		t.Errorf("parseJSONString(null): want nil, got %#v", v)
	}
}

func TestParseJSONString_TypeRoundtrip(t *testing.T) {
	cases := []struct {
		in   string
		want interface{}
	}{
		{`true`, true},
		{`false`, false},
		{`42`, float64(42)},
		{`"hello"`, "hello"},
	}
	for _, tc := range cases {
		got, err := parseJSONString(tc.in)
		if err != nil {
			t.Fatalf("parseJSONString(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseJSONString(%q): got %#v want %#v", tc.in, got, tc.want)
		}
	}
}

func TestJSONScalarToString_RoundTrip(t *testing.T) {
	s, err := jsonScalarToString(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatalf("jsonScalarToString returned err: %v", err)
	}
	if s == "" {
		t.Fatal("expected non-empty JSON output")
	}
	// canonicalize and assert sort
	c, _ := canonicalJSON(s)
	if c != `{"a":2,"b":1}` {
		t.Errorf("canonical round-trip: got %q", c)
	}
}
