package provider

import "sort"

// Header reconciliation for the ADR-056 configuration reshape.
//
// Since ADR-056 the SDK surfaces request headers as an unordered
// map[string]string (the wire carries them as a flat object), whereas the
// provider schema models them as an order-sensitive ListNestedAttribute. A
// naive projection of the map into a list would pick a nondeterministic order
// and churn the plan on every refresh. reorderHeaders pins the projected list
// to a reference order — the plan's order on create/update, the prior state's
// order on read — so a refreshed header list matches the order the practitioner
// declared and produces no spurious diff.

// reorderHeaders returns cur reordered to follow the header-name order in ref.
// Names present in cur but absent from ref are appended in lexicographic order
// (a stable fallback, e.g. on import where there is no prior order); ref names
// absent from cur are dropped (the server no longer returns them). nameOf
// extracts a header's name from the element type.
func reorderHeaders[T any](ref, cur []T, nameOf func(T) string) []T {
	if len(cur) <= 1 {
		return cur
	}
	byName := make(map[string]T, len(cur))
	for _, h := range cur {
		byName[nameOf(h)] = h
	}
	out := make([]T, 0, len(cur))
	seen := make(map[string]struct{}, len(cur))
	for _, r := range ref {
		name := nameOf(r)
		if _, already := seen[name]; already {
			continue
		}
		if h, ok := byName[name]; ok {
			out = append(out, h)
			seen[name] = struct{}{}
		}
	}
	extra := make([]string, 0, len(cur))
	for name := range byName {
		if _, ok := seen[name]; !ok {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, byName[name])
	}
	return out
}
