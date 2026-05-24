package provider

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// jsonScalarToString round-trips a Go value through `json.Marshal` so that
// the same wire-form representation is stored in Terraform state for both
// the user's planned value and the server's returned value.
//
// Without canonicalization the planned `"true"` and the SDK-returned
// `true` (un-quoted boolean) would diff on every plan. We accept any
// JSON-encodable input and emit the canonical encoding.
func jsonScalarToString(v interface{}) (string, error) {
	if v == nil {
		return "null", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling value as JSON: %w", err)
	}
	return string(b), nil
}

// parseJSONString decodes a Terraform-supplied JSON-encoded string into a
// native Go value the SDK will accept. The empty string and a literal
// "null" both decode to a nil value, which the SDK treats as "no value".
func parseJSONString(s string) (interface{}, error) {
	if s == "" {
		return nil, nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, fmt.Errorf("input is not valid JSON: %w", err)
	}
	return v, nil
}

// canonicalJSON re-encodes a JSON string so equal-but-formatted differently
// inputs collapse to the same state — protects against churn on plans
// where the user wrote `{"a":1,"b":2}` and the server returned
// `{"b": 2, "a": 1}`.
func canonicalJSON(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "", err
	}
	return marshalCanonical(v)
}

// marshalCanonical produces a stable, key-sorted JSON encoding of v so
// `{"b":1,"a":2}` and `{"a":2,"b":1}` produce identical state values.
func marshalCanonical(v interface{}) (string, error) {
	b, err := jsonMarshalSorted(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonMarshalSorted(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var out []byte
		out = append(out, '{')
		for i, k := range keys {
			if i > 0 {
				out = append(out, ',')
			}
			kb, _ := json.Marshal(k)
			out = append(out, kb...)
			out = append(out, ':')
			vb, err := jsonMarshalSorted(val[k])
			if err != nil {
				return nil, err
			}
			out = append(out, vb...)
		}
		out = append(out, '}')
		return out, nil
	case []interface{}:
		var out []byte
		out = append(out, '[')
		for i, item := range val {
			if i > 0 {
				out = append(out, ',')
			}
			ib, err := jsonMarshalSorted(item)
			if err != nil {
				return nil, err
			}
			out = append(out, ib...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return json.Marshal(v)
	}
}

// stringMapFromJSONStringMap parses a `map[string]string` where every
// value is a JSON-encoded scalar/object into the `map[string]interface{}`
// shape the SDK accepts. Used for config items and per-environment
// overrides. The error path attaches the offending key to a Terraform
// attribute path so the user gets an actionable diagnostic.
func stringMapFromJSONStringMap(m map[string]string, attr path.Path, diags *diag.Diagnostics) map[string]interface{} {
	if len(m) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		parsed, err := parseJSONString(v)
		if err != nil {
			diags.AddAttributeError(attr.AtMapKey(k), "Invalid JSON value", err.Error())
			continue
		}
		out[k] = parsed
	}
	return out
}

// stringMapToJSONStringMap turns a `map[string]interface{}` back into
// `map[string]string` (each value canonical-JSON-encoded) for storing in
// Terraform state.
func stringMapToJSONStringMap(m map[string]interface{}) (map[string]string, error) {
	if len(m) == 0 {
		return map[string]string{}, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, err := marshalCanonical(v)
		if err != nil {
			return nil, fmt.Errorf("encoding value for key %q: %w", k, err)
		}
		out[k] = s
	}
	return out, nil
}

// stringOrNull returns the *string for an Optional+Nullable schema slot,
// turning the framework's null-string into a real Go nil.
func stringOrNull(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

// stringPointerToTypes maps an optional Go pointer back to the framework's
// String type, preserving null vs. empty distinction.
func stringPointerToTypes(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}
