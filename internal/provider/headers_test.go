package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func hdr(name, value string) jobHeader {
	return jobHeader{Name: types.StringValue(name), Value: types.StringValue(value)}
}

func headerNames(hs []jobHeader) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Name.ValueString())
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReorderHeaders(t *testing.T) {
	cases := []struct {
		name string
		ref  []string // names, in the order the practitioner declared
		cur  []string // names, as a server map would surface them (any order)
		want []string // expected list order after reconciliation
	}{
		{
			name: "scrambled server order is restored to the declared order",
			ref:  []string{"A", "B", "C"},
			cur:  []string{"C", "A", "B"},
			want: []string{"A", "B", "C"},
		},
		{
			name: "a header not in the reference is appended in name order",
			ref:  []string{"A", "B"},
			cur:  []string{"B", "A", "C"},
			want: []string{"A", "B", "C"},
		},
		{
			name: "multiple unreferenced headers append sorted, after the referenced ones",
			ref:  []string{"M"},
			cur:  []string{"Z", "M", "A"},
			want: []string{"M", "A", "Z"},
		},
		{
			name: "a reference header the server no longer returns is dropped",
			ref:  []string{"A", "B", "C"},
			cur:  []string{"B", "A"},
			want: []string{"A", "B"},
		},
		{
			name: "no reference order falls back to sorted (e.g. on import)",
			ref:  nil,
			cur:  []string{"B", "A"},
			want: []string{"A", "B"},
		},
		{
			name: "a single header short-circuits unchanged",
			ref:  []string{"B"},
			cur:  []string{"A"},
			want: []string{"A"},
		},
		{
			name: "empty current yields empty",
			ref:  []string{"A"},
			cur:  nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ref := make([]jobHeader, 0, len(tc.ref))
			for _, n := range tc.ref {
				ref = append(ref, hdr(n, n+"-ref"))
			}
			cur := make([]jobHeader, 0, len(tc.cur))
			for _, n := range tc.cur {
				cur = append(cur, hdr(n, n+"-cur"))
			}
			got := reorderHeaders(ref, cur, jobHeaderName)
			if !equalStrings(headerNames(got), tc.want) {
				t.Errorf("order: got %v want %v", headerNames(got), tc.want)
			}
			// The reconciler must keep CURRENT values (server-authoritative),
			// never substitute the reference's values.
			for _, h := range got {
				if want := h.Name.ValueString() + "-cur"; h.Value.ValueString() != want {
					t.Errorf("value for %q: got %q want %q (must keep current, not reference)",
						h.Name.ValueString(), h.Value.ValueString(), want)
				}
			}
		})
	}
}

func TestReconcileJobHeaderOrder(t *testing.T) {
	// The declared (ref) order is B,A for the base config and Y,X for the
	// per-env override; the freshly projected (server) order is sorted A,B / X,Y.
	// reconcileJobHeaderOrder must restore the declared order on both.
	ref := &jobResourceModel{
		Configuration: &jobConfigurationModel{Headers: []jobHeader{hdr("B", "2"), hdr("A", "1")}},
		Environments: map[string]jobEnvOverride{
			"production": {Configuration: &jobConfigurationModel{Headers: []jobHeader{hdr("Y", "y"), hdr("X", "x")}}},
		},
	}
	dst := &jobResourceModel{
		Configuration: &jobConfigurationModel{Headers: []jobHeader{hdr("A", "1"), hdr("B", "2")}},
		Environments: map[string]jobEnvOverride{
			"production": {Configuration: &jobConfigurationModel{Headers: []jobHeader{hdr("X", "x"), hdr("Y", "y")}}},
		},
	}
	reconcileJobHeaderOrder(ref, dst)
	if got := headerNames(dst.Configuration.Headers); !equalStrings(got, []string{"B", "A"}) {
		t.Errorf("base header order not reconciled: %v", got)
	}
	if got := headerNames(dst.Environments["production"].Configuration.Headers); !equalStrings(got, []string{"Y", "X"}) {
		t.Errorf("per-env header order not reconciled: %v", got)
	}
}
