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
		"id", "name", "max_retries", "backoff", "delay_seconds",
		"max_delay_seconds", "retry_on", "created_at", "updated_at", "version",
	} {
		if _, ok := attrs[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestRetryOnFromModel_FullValues(t *testing.T) {
	m := &retryOnModel{
		Statuses: []types.Int64{types.Int64Value(429), types.Int64Value(503)},
		Reasons:  []types.String{types.StringValue("CONNECTION_ERROR"), types.StringValue("TIMEOUT")},
	}
	out := retryOnFromModel(m)
	if len(out.Statuses) != 2 || out.Statuses[0] != 429 || out.Statuses[1] != 503 {
		t.Errorf("statuses not converted: %+v", out.Statuses)
	}
	if len(out.Reasons) != 2 || out.Reasons[0] != smplkit.RetryReasonConnectionError || out.Reasons[1] != smplkit.RetryReasonTimeout {
		t.Errorf("reasons not converted: %+v", out.Reasons)
	}
}

func TestRetryOnFromModel_Nil(t *testing.T) {
	out := retryOnFromModel(nil)
	if len(out.Statuses) != 0 || len(out.Reasons) != 0 {
		t.Errorf("nil model should yield empty RetryOn: %+v", out)
	}
}

func TestRetryOnFromModel_SkipsUnknownElements(t *testing.T) {
	// Unknown list elements must be skipped rather than projected as zero
	// values onto the wire.
	m := &retryOnModel{
		Statuses: []types.Int64{types.Int64Value(429), types.Int64Unknown()},
		Reasons:  []types.String{types.StringValue("TIMEOUT"), types.StringNull()},
	}
	out := retryOnFromModel(m)
	if len(out.Statuses) != 1 || out.Statuses[0] != 429 {
		t.Errorf("unknown status not skipped: %+v", out.Statuses)
	}
	if len(out.Reasons) != 1 || out.Reasons[0] != smplkit.RetryReasonTimeout {
		t.Errorf("null reason not skipped: %+v", out.Reasons)
	}
}

func TestBuildRetryPolicyOptions(t *testing.T) {
	m := &retryPolicyResourceModel{
		MaxDelaySeconds: types.Int64Value(120),
		RetryOn: &retryOnModel{
			Statuses: []types.Int64{types.Int64Value(503)},
			Reasons:  []types.String{types.StringValue("NON_SUCCESS_STATUS")},
		},
	}
	policy := &smplkit.RetryPolicy{}
	for _, opt := range buildRetryPolicyOptions(m) {
		opt(policy)
	}
	if policy.MaxDelaySeconds == nil || *policy.MaxDelaySeconds != 120 {
		t.Errorf("max_delay_seconds option not applied: %v", policy.MaxDelaySeconds)
	}
	if len(policy.RetryOn.Statuses) != 1 || policy.RetryOn.Statuses[0] != 503 {
		t.Errorf("retry_on statuses option not applied: %+v", policy.RetryOn)
	}
	if len(policy.RetryOn.Reasons) != 1 || policy.RetryOn.Reasons[0] != smplkit.RetryReasonNonSuccessStatus {
		t.Errorf("retry_on reasons option not applied: %+v", policy.RetryOn)
	}
}

func TestBuildRetryPolicyOptions_OmitsUnsetOptionals(t *testing.T) {
	// A null max_delay_seconds and an absent retry_on must leave the SDK
	// defaults intact (nil pointer, empty RetryOn).
	m := &retryPolicyResourceModel{
		MaxDelaySeconds: types.Int64Null(),
		RetryOn:         nil,
	}
	policy := &smplkit.RetryPolicy{}
	for _, opt := range buildRetryPolicyOptions(m) {
		opt(policy)
	}
	if policy.MaxDelaySeconds != nil {
		t.Errorf("null max_delay_seconds should not be applied, got %v", *policy.MaxDelaySeconds)
	}
	if len(policy.RetryOn.Statuses) != 0 || len(policy.RetryOn.Reasons) != 0 {
		t.Errorf("absent retry_on should leave RetryOn empty, got %+v", policy.RetryOn)
	}
}

func TestApplyRetryPolicyToModel_FullValues(t *testing.T) {
	maxDelay := 90
	ver := 4
	created := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	policy := &smplkit.RetryPolicy{
		ID:              "aggressive-retry",
		Name:            "Aggressive retry",
		MaxRetries:      5,
		Backoff:         smplkit.BackoffExponential,
		DelaySeconds:    2,
		MaxDelaySeconds: &maxDelay,
		RetryOn: smplkit.RetryOn{
			Statuses: []int{429, 503},
			Reasons:  []smplkit.RetryReason{smplkit.RetryReasonConnectionError},
		},
		CreatedAt: &created,
		UpdatedAt: &updated,
		Version:   &ver,
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
	if m.RetryOn == nil || len(m.RetryOn.Statuses) != 2 || m.RetryOn.Statuses[0].ValueInt64() != 429 {
		t.Errorf("retry_on statuses not projected: %+v", m.RetryOn)
	}
	if m.RetryOn == nil || len(m.RetryOn.Reasons) != 1 || m.RetryOn.Reasons[0].ValueString() != "CONNECTION_ERROR" {
		t.Errorf("retry_on reasons not projected: %+v", m.RetryOn)
	}
	if m.CreatedAt.ValueString() != "2026-06-01T00:00:00Z" || m.UpdatedAt.ValueString() != "2026-06-02T00:00:00Z" {
		t.Errorf("timestamps: created=%q updated=%q", m.CreatedAt.ValueString(), m.UpdatedAt.ValueString())
	}
	if m.Version.ValueInt64() != 4 {
		t.Errorf("version: %d", m.Version.ValueInt64())
	}
}

func TestApplyRetryPolicyToModel_NullsForUnsetFields(t *testing.T) {
	// An uncapped delay (nil MaxDelaySeconds) and an empty RetryOn (both
	// lists empty) must project to null so an omitted Optional attribute
	// round-trips cleanly rather than reporting null -> {} after apply.
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
	if m.RetryOn != nil {
		t.Errorf("retry_on should be null when empty, got %+v", m.RetryOn)
	}
	if !m.Version.IsNull() {
		t.Errorf("version should be null when unset")
	}
}

func TestRetryOnModelFromSDK_RoundTrip(t *testing.T) {
	// A non-empty RetryOn projects back into a model that round-trips through
	// retryOnFromModel to the same SDK shape.
	in := smplkit.RetryOn{
		Statuses: []int{500},
		Reasons:  []smplkit.RetryReason{smplkit.RetryReasonTimeout, smplkit.RetryReasonConnectionError},
	}
	model := retryOnModelFromSDK(in)
	if model == nil {
		t.Fatal("non-empty RetryOn should project to a non-nil model")
	}
	out := retryOnFromModel(model)
	if len(out.Statuses) != 1 || out.Statuses[0] != 500 {
		t.Errorf("statuses round-trip: %+v", out.Statuses)
	}
	if len(out.Reasons) != 2 || out.Reasons[0] != smplkit.RetryReasonTimeout {
		t.Errorf("reasons round-trip: %+v", out.Reasons)
	}
}

func TestRetryOnModelFromSDK_EmptyIsNil(t *testing.T) {
	if model := retryOnModelFromSDK(smplkit.RetryOn{}); model != nil {
		t.Errorf("empty RetryOn should project to nil, got %+v", model)
	}
}
