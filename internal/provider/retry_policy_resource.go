package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// validBackoffStrategies mirrors the SDK's Backoff enum; kept identical to
// the wire strings so casing doesn't drift between schema and SDK.
var validBackoffStrategies = []string{"exponential", "fixed"}

// NewRetryPolicyResource is the factory the provider hands to Terraform for
// the `smplkit_retry_policy` resource.
func NewRetryPolicyResource() resource.Resource {
	return &retryPolicyResource{}
}

type retryPolicyResource struct {
	client *smplkit.SmplClient
}

type retryPolicyResourceModel struct {
	ID                     types.String   `tfsdk:"id"`
	Name                   types.String   `tfsdk:"name"`
	MaxRetries             types.Int64    `tfsdk:"max_retries"`
	Backoff                types.String   `tfsdk:"backoff"`
	DelaySeconds           types.Int64    `tfsdk:"delay_seconds"`
	MaxDelaySeconds        types.Int64    `tfsdk:"max_delay_seconds"`
	RetryOnTimeout         types.Bool     `tfsdk:"retry_on_timeout"`
	RetryOnConnectionError types.Bool     `tfsdk:"retry_on_connection_error"`
	RetryStatuses          []types.String `tfsdk:"retry_statuses"`
	RetryStatusesExcept    []types.String `tfsdk:"retry_statuses_except"`
	CreatedAt              types.String   `tfsdk:"created_at"`
	UpdatedAt              types.String   `tfsdk:"updated_at"`
	Version                types.Int64    `tfsdk:"version"`
}

func (r *retryPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_retry_policy"
}

func (r *retryPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A named, reusable retry policy for failed job runs. Reference it from a " +
			"`smplkit_job`'s `retry_policy` (and optionally override it per environment). A job that " +
			"references no policy uses the built-in `Default` policy, which never retries. Retry policies " +
			"are account-global — never environment-scoped. The `id` is caller-supplied, immutable, and " +
			"doubles as the import id. Updates are full-replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The retry-policy key (e.g. `aggressive-retry`). Caller-supplied at create and " +
					"immutable. Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name shown in the smplkit console.",
			},
			"max_retries": schema.Int64Attribute{
				Required: true,
				Description: "How many times a failed run is retried after the initial attempt — `3` means up " +
					"to 4 attempts total. `0` disables retries. Maximum 10.",
				Validators: []validator.Int64{int64validator.Between(0, 10)},
			},
			"backoff": schema.StringAttribute{
				Required: true,
				Description: "How the wait between retries grows. One of `exponential` (double the wait each " +
					"retry, capped at `max_delay_seconds`) or `fixed` (a constant `delay_seconds` before every " +
					"retry).",
				Validators: []validator.String{stringvalidator.OneOf(validBackoffStrategies...)},
			},
			"delay_seconds": schema.Int64Attribute{
				Required: true,
				Description: "The wait before a retry, in seconds — the constant wait for `fixed` backoff, or the " +
					"base that doubles each retry for `exponential`.",
			},
			"max_delay_seconds": schema.Int64Attribute{
				Optional: true,
				Description: "Ceiling on the wait between retries, for `exponential` backoff only. Omit to leave " +
					"it uncapped; do not set it for `fixed` backoff.",
			},
			"retry_statuses": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Response status patterns to retry when a run fails because the response did not " +
					"match the job's success status. Each is an exact 3-digit code (`429`) or a class " +
					"(`1xx`, `2xx`, `3xx`, `4xx`, `5xx`) — e.g. `[\"429\", \"5xx\"]` to retry rate-limit and any " +
					"server error. Omit to retry on no status.",
			},
			"retry_statuses_except": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Status patterns to subtract from `retry_statuses`, using the same exact-code or " +
					"class syntax — a status matching both lists is not retried (except wins on overlap). E.g. " +
					"`retry_statuses = [\"5xx\"]` with `retry_statuses_except = [\"501\"]` retries every server " +
					"error except 501.",
			},
			"retry_on_timeout": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Retry a run that failed because the request did not complete within the job's " +
					"timeout. Defaults to `false`.",
			},
			"retry_on_connection_error": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "Retry a run that failed because the destination could not be reached (DNS, " +
					"connection refused, TLS, or transport error). Defaults to `false`.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the policy was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
				Description: "RFC3339 timestamp set by the server on every write. Recomputed on every apply, so " +
					"plans involving updates show this as `(known after apply)`.",
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic counter bumped by the server on every write, starting at 1.",
			},
		},
	}
}

func (r *retryPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*smplkit.SmplClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.SmplClient, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *retryPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data retryPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy := r.client.Jobs().RetryPolicies().New(
		data.ID.ValueString(),
		data.Name.ValueString(),
		int(data.MaxRetries.ValueInt64()),
		smplkit.Backoff(data.Backoff.ValueString()),
		int(data.DelaySeconds.ValueInt64()),
		buildRetryPolicyOptions(&data)...,
	)
	if err := policy.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_retry_policy %q", data.ID.ValueString()), err)
		return
	}

	applyRetryPolicyToModel(policy, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *retryPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data retryPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := r.client.Jobs().RetryPolicies().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_retry_policy %q", data.ID.ValueString()), err)
		return
	}

	applyRetryPolicyToModel(policy, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *retryPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state retryPolicyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get-mutate-save full-replace: fetch the live policy, overwrite the
	// mutable fields from the plan, and PUT it back. `id` is immutable, so
	// it is left untouched.
	policy, err := r.client.Jobs().RetryPolicies().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_retry_policy %q before update", state.ID.ValueString()), err)
		return
	}

	policy.Name = plan.Name.ValueString()
	policy.MaxRetries = int(plan.MaxRetries.ValueInt64())
	policy.Backoff = smplkit.Backoff(plan.Backoff.ValueString())
	policy.DelaySeconds = int(plan.DelaySeconds.ValueInt64())
	if isKnownInt(plan.MaxDelaySeconds) {
		v := int(plan.MaxDelaySeconds.ValueInt64())
		policy.MaxDelaySeconds = &v
	} else {
		policy.MaxDelaySeconds = nil
	}
	policy.RetryStatuses = stringListToSlice(plan.RetryStatuses)
	policy.RetryStatusesExcept = stringListToSlice(plan.RetryStatusesExcept)
	policy.RetryOnTimeout = isKnownBool(plan.RetryOnTimeout) && plan.RetryOnTimeout.ValueBool()
	policy.RetryOnConnectionError = isKnownBool(plan.RetryOnConnectionError) && plan.RetryOnConnectionError.ValueBool()

	if err := policy.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_retry_policy %q", state.ID.ValueString()), err)
		return
	}

	applyRetryPolicyToModel(policy, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *retryPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data retryPolicyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Jobs().RetryPolicies().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_retry_policy %q", data.ID.ValueString()), err)
	}
}

func (r *retryPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// buildRetryPolicyOptions threads the optional attributes (max delay, retry
// conditions) through the SDK's With* options. max_retries, backoff, and
// delay_seconds are Required, so they go through New directly.
func buildRetryPolicyOptions(data *retryPolicyResourceModel) []smplkit.RetryPolicyOption {
	opts := []smplkit.RetryPolicyOption{}
	if isKnownInt(data.MaxDelaySeconds) {
		opts = append(opts, smplkit.WithRetryPolicyMaxDelaySeconds(int(data.MaxDelaySeconds.ValueInt64())))
	}
	if statuses := stringListToSlice(data.RetryStatuses); len(statuses) > 0 {
		opts = append(opts, smplkit.WithRetryPolicyRetryStatuses(statuses))
	}
	if except := stringListToSlice(data.RetryStatusesExcept); len(except) > 0 {
		opts = append(opts, smplkit.WithRetryPolicyRetryStatusesExcept(except))
	}
	if isKnownBool(data.RetryOnTimeout) && data.RetryOnTimeout.ValueBool() {
		opts = append(opts, smplkit.WithRetryPolicyRetryOnTimeout(true))
	}
	if isKnownBool(data.RetryOnConnectionError) && data.RetryOnConnectionError.ValueBool() {
		opts = append(opts, smplkit.WithRetryPolicyRetryOnConnectionError(true))
	}
	return opts
}

// stringListToSlice flattens a schema string list into a plain []string,
// skipping null/unknown elements. Returns nil for an empty or absent list.
func stringListToSlice(list []types.String) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, v := range list {
		if isKnown(v) {
			out = append(out, v.ValueString())
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringSliceToList projects a plain []string into a schema string list,
// preserving order. Returns nil for an empty slice so an omitted (null) list
// round-trips rather than reporting null -> [] after apply.
func stringSliceToList(s []string) []types.String {
	if len(s) == 0 {
		return nil
	}
	out := make([]types.String, 0, len(s))
	for _, v := range s {
		out = append(out, types.StringValue(v))
	}
	return out
}

// applyRetryPolicyToModel copies a server-authoritative RetryPolicy into the
// Terraform model.
func applyRetryPolicyToModel(policy *smplkit.RetryPolicy, model *retryPolicyResourceModel) {
	model.ID = types.StringValue(policy.ID)
	model.Name = types.StringValue(policy.Name)
	model.MaxRetries = types.Int64Value(int64(policy.MaxRetries))
	model.Backoff = types.StringValue(string(policy.Backoff))
	model.DelaySeconds = types.Int64Value(int64(policy.DelaySeconds))
	if policy.MaxDelaySeconds != nil {
		model.MaxDelaySeconds = types.Int64Value(int64(*policy.MaxDelaySeconds))
	} else {
		model.MaxDelaySeconds = types.Int64Null()
	}
	model.RetryOnTimeout = types.BoolValue(policy.RetryOnTimeout)
	model.RetryOnConnectionError = types.BoolValue(policy.RetryOnConnectionError)
	model.RetryStatuses = stringSliceToList(policy.RetryStatuses)
	model.RetryStatusesExcept = stringSliceToList(policy.RetryStatusesExcept)
	model.CreatedAt = timePointerToString(policy.CreatedAt)
	model.UpdatedAt = timePointerToString(policy.UpdatedAt)
	if policy.Version != nil {
		model.Version = types.Int64Value(int64(*policy.Version))
	} else {
		model.Version = types.Int64Null()
	}
}
