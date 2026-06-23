package provider

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

func TestRetryPolicyResource_Schema(t *testing.T) {
	r := NewRetryPolicyResource()
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	attrs := resp.Schema.Attributes
	for _, name := range []string{
		"id", "name", "max_retries", "backoff", "delay_seconds", "max_delay_seconds",
		"retry_statuses", "retry_statuses_except", "retry_on_timeout",
		"retry_on_connection_error", "created_at", "updated_at", "version",
	} {
		if _, ok := attrs[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
	// The legacy nested retry_on block is gone.
	if _, ok := attrs["retry_on"]; ok {
		t.Error("legacy retry_on attribute must be removed")
	}
}

func TestStringListToSlice(t *testing.T) {
	out := stringListToSlice([]types.String{types.StringValue("429"), types.StringValue("5xx")})
	if len(out) != 2 || out[0] != "429" || out[1] != "5xx" {
		t.Errorf("not converted in order: %#v", out)
	}
	// Unknown/null elements are skipped.
	skipped := stringListToSlice([]types.String{types.StringValue("429"), types.StringUnknown(), types.StringNull()})
	if len(skipped) != 1 || skipped[0] != "429" {
		t.Errorf("unknown/null not skipped: %#v", skipped)
	}
	// Empty / all-skipped yields nil.
	if got := stringListToSlice(nil); got != nil {
		t.Errorf("nil input should yield nil: %#v", got)
	}
	if got := stringListToSlice([]types.String{types.StringNull()}); got != nil {
		t.Errorf("all-null input should yield nil: %#v", got)
	}
}

func TestStringSliceToList(t *testing.T) {
	out := stringSliceToList([]string{"429", "5xx"})
	if len(out) != 2 || out[0].ValueString() != "429" || out[1].ValueString() != "5xx" {
		t.Errorf("not projected in order: %#v", out)
	}
	if got := stringSliceToList(nil); got != nil {
		t.Errorf("empty slice should project to nil: %#v", got)
	}
}

func TestBuildRetryPolicyOptions(t *testing.T) {
	m := &retryPolicyResourceModel{
		MaxDelaySeconds:        types.Int64Value(120),
		RetryStatuses:          []types.String{types.StringValue("429"), types.StringValue("5xx")},
		RetryStatusesExcept:    []types.String{types.StringValue("501")},
		RetryOnTimeout:         types.BoolValue(true),
		RetryOnConnectionError: types.BoolValue(true),
	}
	policy := &smplkit.RetryPolicy{}
	for _, opt := range buildRetryPolicyOptions(m) {
		opt(policy)
	}
	if policy.MaxDelaySeconds == nil || *policy.MaxDelaySeconds != 120 {
		t.Errorf("max_delay_seconds option not applied: %v", policy.MaxDelaySeconds)
	}
	if len(policy.RetryStatuses) != 2 || policy.RetryStatuses[0] != "429" || policy.RetryStatuses[1] != "5xx" {
		t.Errorf("retry_statuses option not applied: %+v", policy.RetryStatuses)
	}
	if len(policy.RetryStatusesExcept) != 1 || policy.RetryStatusesExcept[0] != "501" {
		t.Errorf("retry_statuses_except option not applied: %+v", policy.RetryStatusesExcept)
	}
	if !policy.RetryOnTimeout || !policy.RetryOnConnectionError {
		t.Errorf("retry-on booleans not applied: timeout=%v conn=%v", policy.RetryOnTimeout, policy.RetryOnConnectionError)
	}
}

func TestBuildRetryPolicyOptions_OmitsUnsetOptionals(t *testing.T) {
	// A null max_delay_seconds, absent status lists, and false booleans must
	// leave the SDK defaults intact (nil pointer, empty slices, false bools).
	m := &retryPolicyResourceModel{
		MaxDelaySeconds:        types.Int64Null(),
		RetryOnTimeout:         types.BoolValue(false),
		RetryOnConnectionError: types.BoolNull(),
	}
	policy := &smplkit.RetryPolicy{}
	for _, opt := range buildRetryPolicyOptions(m) {
		opt(policy)
	}
	if policy.MaxDelaySeconds != nil {
		t.Errorf("null max_delay_seconds should not be applied, got %v", *policy.MaxDelaySeconds)
	}
	if len(policy.RetryStatuses) != 0 || len(policy.RetryStatusesExcept) != 0 {
		t.Errorf("absent status lists should leave them empty, got %+v / %+v", policy.RetryStatuses, policy.RetryStatusesExcept)
	}
	if policy.RetryOnTimeout || policy.RetryOnConnectionError {
		t.Errorf("false/absent booleans should leave them false: timeout=%v conn=%v", policy.RetryOnTimeout, policy.RetryOnConnectionError)
	}
}

func TestApplyRetryPolicyToModel_FullValues(t *testing.T) {
	maxDelay := 90
	ver := 4
	created := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	policy := &smplkit.RetryPolicy{
		ID:                     "aggressive-retry",
		Name:                   "Aggressive retry",
		MaxRetries:             5,
		Backoff:                smplkit.BackoffExponential,
		DelaySeconds:           2,
		MaxDelaySeconds:        &maxDelay,
		RetryOnTimeout:         true,
		RetryOnConnectionError: true,
		RetryStatuses:          []string{"429", "5xx"},
		RetryStatusesExcept:    []string{"501"},
		CreatedAt:              &created,
		UpdatedAt:              &updated,
		Version:                &ver,
	}
	var m retryPolicyResourceModel
	applyRetryPolicyToModel(policy, &m)
	if m.ID.ValueString() != "aggressive-retry" || m.Name.ValueString() != "Aggressive retry" {
		t.Errorf("id/name: %+v", m)
	}
	if m.MaxRetries.ValueInt64() != 5 || m.Backoff.ValueString() != "exponential" || m.DelaySeconds.ValueInt64() != 2 {
		t.Errorf("scalars: %+v", m)
	}
	if m.MaxDelaySeconds.ValueInt64() != 90 {
		t.Errorf("max_delay_seconds: %d", m.MaxDelaySeconds.ValueInt64())
	}
	if !m.RetryOnTimeout.ValueBool() || !m.RetryOnConnectionError.ValueBool() {
		t.Errorf("retry-on booleans not projected: %+v", m)
	}
	if len(m.RetryStatuses) != 2 || m.RetryStatuses[0].ValueString() != "429" || m.RetryStatuses[1].ValueString() != "5xx" {
		t.Errorf("retry_statuses not projected: %+v", m.RetryStatuses)
	}
	if len(m.RetryStatusesExcept) != 1 || m.RetryStatusesExcept[0].ValueString() != "501" {
		t.Errorf("retry_statuses_except not projected: %+v", m.RetryStatusesExcept)
	}
	if m.CreatedAt.ValueString() != "2026-06-01T00:00:00Z" || m.UpdatedAt.ValueString() != "2026-06-02T00:00:00Z" {
		t.Errorf("timestamps: created=%q updated=%q", m.CreatedAt.ValueString(), m.UpdatedAt.ValueString())
	}
	if m.Version.ValueInt64() != 4 {
		t.Errorf("version: %d", m.Version.ValueInt64())
	}
}

func TestApplyRetryPolicyToModel_NullsForUnsetFields(t *testing.T) {
	// An uncapped delay (nil MaxDelaySeconds) and empty status lists must project
	// to null so an omitted Optional attribute round-trips cleanly. The two
	// booleans are Optional+Computed (default false), so they project to their
	// concrete value.
	policy := &smplkit.RetryPolicy{
		ID:           "fixed-retry",
		Name:         "Fixed retry",
		MaxRetries:   0,
		Backoff:      smplkit.BackoffFixed,
		DelaySeconds: 10,
	}
	var m retryPolicyResourceModel
	applyRetryPolicyToModel(policy, &m)
	if !m.MaxDelaySeconds.IsNull() {
		t.Errorf("max_delay_seconds should be null when uncapped, got %d", m.MaxDelaySeconds.ValueInt64())
	}
	if m.RetryStatuses != nil || m.RetryStatusesExcept != nil {
		t.Errorf("status lists should be nil when empty, got %+v / %+v", m.RetryStatuses, m.RetryStatusesExcept)
	}
	if m.RetryOnTimeout.ValueBool() || m.RetryOnConnectionError.ValueBool() {
		t.Errorf("unset booleans should project to false, got timeout=%v conn=%v", m.RetryOnTimeout, m.RetryOnConnectionError)
	}
	if !m.Version.IsNull() {
		t.Errorf("version should be null when unset")
	}
}
