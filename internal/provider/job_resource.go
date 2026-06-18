package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// validJobMethods mirrors the SDK's JobHttpMethod enum; kept identical to
// the wire strings so casing doesn't drift between schema and SDK.
var validJobMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// validJobConcurrencyPolicies is the concurrency-policy enum. "ALLOW" is
// the only value the jobs service accepts today.
var validJobConcurrencyPolicies = []string{"ALLOW"}

// NewJobResource is the factory the provider hands to Terraform for the
// `smplkit_job` resource.
func NewJobResource() resource.Resource {
	return &jobResource{}
}

type jobResource struct {
	client *smplkit.SmplClient
}

type jobConfigurationModel struct {
	URL           types.String `tfsdk:"url"`
	Method        types.String `tfsdk:"method"`
	Headers       []jobHeader  `tfsdk:"headers"`
	Body          types.String `tfsdk:"body"`
	SuccessStatus types.String `tfsdk:"success_status"`
	Timeout       types.Int64  `tfsdk:"timeout"`
	TLSVerify     types.Bool   `tfsdk:"tls_verify"`
	CACert        types.String `tfsdk:"ca_cert"`
}

type jobHeader struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// jobEnvOverride is the nested object inside the `environments` map.
// `enabled` toggles whether the job runs in this environment (the base
// `enabled` is a read-only roll-up the server derives, so enablement lives
// entirely here); `configuration` is an optional per-environment override
// that fully replaces the base configuration in that environment — omit it
// to inherit the base.
type jobEnvOverride struct {
	Enabled       types.Bool             `tfsdk:"enabled"`
	Configuration *jobConfigurationModel `tfsdk:"configuration"`
}

type jobResourceModel struct {
	ID                types.String              `tfsdk:"id"`
	Name              types.String              `tfsdk:"name"`
	Description       types.String              `tfsdk:"description"`
	Enabled           types.Bool                `tfsdk:"enabled"`
	Recurring         types.Bool                `tfsdk:"recurring"`
	Type              types.String              `tfsdk:"type"`
	Schedule          types.String              `tfsdk:"schedule"`
	ConcurrencyPolicy types.String              `tfsdk:"concurrency_policy"`
	Environments      map[string]jobEnvOverride `tfsdk:"environments"`
	Configuration     *jobConfigurationModel    `tfsdk:"configuration"`
	NextRunAt         types.String              `tfsdk:"next_run_at"`
	CreatedAt         types.String              `tfsdk:"created_at"`
	UpdatedAt         types.String              `tfsdk:"updated_at"`
	Version           types.Int64               `tfsdk:"version"`
}

func (r *jobResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_job"
}

func (r *jobResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A scheduled job: an HTTP request the platform fires on a schedule, recording the history of " +
			"each run. The `id` is caller-supplied, immutable, and doubles as the import id. Enablement is " +
			"per-environment via the `environments` map — a recurring job fires only in the environments it is " +
			"enabled in. Updates are full-replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The job key (e.g. `nightly-cache-warm`). Caller-supplied at create and immutable. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name shown in the smplkit console.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional free-text description.",
			},
			"enabled": schema.BoolAttribute{
				Computed: true,
				Description: "Read-only roll-up: `true` when the job is enabled in at least one environment. " +
					"Derived server-side from `environments`; set enablement per environment via the " +
					"`environments` map.",
			},
			"recurring": schema.BoolAttribute{
				Computed: true,
				Description: "Read-only: `true` for a recurring (cron) schedule, `false` for a one-off " +
					"(datetime / `now`) schedule. Derived server-side from `schedule`.",
			},
			"type": schema.StringAttribute{
				Computed:      true,
				Description:   "Job type. Only `http` is supported today.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"schedule": schema.StringAttribute{
				Required: true,
				Description: "When the job runs: a 5-field cron expression evaluated in UTC (recurring), an " +
					"ISO-8601 datetime (a one-off run at that instant), or the literal `now` (run once, as soon as " +
					"possible). A datetime or `now` job disables itself after it fires.",
			},
			"concurrency_policy": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString("ALLOW"),
				Description: "How overlapping runs are handled. `ALLOW` (the default and only value today) permits a " +
					"new run to start while a previous one is still in flight.",
				Validators: []validator.String{stringvalidator.OneOf(validJobConcurrencyPolicies...)},
			},
			"environments": schema.MapNestedAttribute{
				Optional: true,
				Description: "Per-environment overrides keyed by environment id (e.g. `production`). " +
					"A recurring job fires in an environment only when that environment's entry sets " +
					"`enabled = true`; an environment with no entry does not run there. Each entry may " +
					"also carry a `configuration` override that fully replaces the base `configuration` " +
					"in that environment. Every referenced environment must already exist for the account.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether the job runs in this environment. Defaults to `false`.",
						},
						"configuration": jobConfigurationSchemaAttribute(false,
							"Optional per-environment HTTP request configuration that fully replaces the base "+
								"`configuration` in this environment. Omit to inherit the base configuration."),
					},
				},
			},
			"configuration": jobConfigurationSchemaAttribute(true,
				"The HTTP request the job performs each time it fires."),
			"next_run_at": schema.StringAttribute{
				Computed: true,
				Description: "RFC3339 timestamp of the next scheduled fire time. Null once a one-off job has fired. " +
					"Recomputed by the server, so it refreshes as the schedule advances.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the job was created.",
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

// jobConfigurationSchemaAttribute builds the SingleNestedAttribute used for
// both the job's base `configuration` and each per-environment override
// `configuration`. required controls whether the block must be supplied
// (true for the base configuration, false for the optional per-environment
// override); description is the block's top-level docstring.
func jobConfigurationSchemaAttribute(required bool, description string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Required:    required,
		Optional:    !required,
		Description: description,
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Absolute `http://` or `https://` URL the job calls when it fires.",
			},
			"method": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "HTTP method. One of `GET`, `POST`, `PUT`, `PATCH`, `DELETE`. Defaults to `POST`.",
				Validators:  []validator.String{stringvalidator.OneOf(validJobMethods...)},
			},
			"headers": schema.ListNestedAttribute{
				Optional: true,
				Description: "Headers attached to every run's request. Values carry credentials and are " +
					"marked sensitive; unlike audit forwarders, the jobs service stores and returns them as " +
					"supplied, so they round-trip cleanly.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Header name.",
						},
						"value": schema.StringAttribute{
							Required:    true,
							Sensitive:   true,
							Description: "Header value (e.g. an auth token).",
						},
					},
				},
			},
			"body": schema.StringAttribute{
				Optional: true,
				Description: "Request body sent on each run. Omit for an empty body. Sent verbatim — pair " +
					"with a matching `Content-Type` header.",
			},
			"success_status": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Response status that counts as success — an exact code (`200`, `204`) or a " +
					"class (`2xx`, `5xx`). Defaults to `2xx`.",
			},
			"timeout": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "Per-run timeout in seconds. A run that does not complete within this many " +
					"seconds fails with reason `TIMEOUT`. Defaults to `30`.",
			},
			"tls_verify": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Description: "Whether the destination's TLS certificate chain is verified. Defaults to " +
					"`true`. Set `false` to skip verification.",
			},
			"ca_cert": schema.StringAttribute{
				Optional: true,
				Description: "Optional PEM-encoded certificate (or bundle) trusted in addition to the system " +
					"CA store. Ignored when `tls_verify` is `false`.",
			},
		},
	}
}

func (r *jobResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *jobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data jobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configuration := jobHTTPConfigFromModel(data.Configuration)
	job := r.client.Jobs().New(
		data.ID.ValueString(),
		data.Name.ValueString(),
		data.Schedule.ValueString(),
		configuration,
		buildJobOptions(&data)...,
	)
	if err := job.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_job %q", data.ID.ValueString()), err)
		return
	}

	applyJobToModel(job, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *jobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data jobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	job, err := r.client.Jobs().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_job %q", data.ID.ValueString()), err)
		return
	}

	applyJobToModel(job, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *jobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state jobResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get-mutate-save full-replace: fetch the live job, overwrite the
	// mutable fields from the plan, and PUT it back. `type` and `id` are
	// immutable, so they are left untouched.
	job, err := r.client.Jobs().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_job %q before update", state.ID.ValueString()), err)
		return
	}

	job.Name = plan.Name.ValueString()
	job.Description = stringOrNull(plan.Description)
	// Full-replace the per-environment override map with the plan's. A nil
	// map clears enablement everywhere (the base `enabled` is a read-only
	// roll-up the server derives).
	job.Environments = jobEnvironmentsFromModel(plan.Environments)
	job.Schedule = plan.Schedule.ValueString()
	if !plan.ConcurrencyPolicy.IsNull() && !plan.ConcurrencyPolicy.IsUnknown() && plan.ConcurrencyPolicy.ValueString() != "" {
		job.ConcurrencyPolicy = plan.ConcurrencyPolicy.ValueString()
	}
	job.Configuration = jobHTTPConfigFromModel(plan.Configuration)

	if err := job.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_job %q", state.ID.ValueString()), err)
		return
	}

	applyJobToModel(job, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *jobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data jobResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Jobs().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_job %q", data.ID.ValueString()), err)
	}
}

func (r *jobResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// buildJobOptions threads the optional/defaulted attributes through the
// SDK's With* options. Enablement is per-environment, so it travels through
// WithJobEnvironments; concurrency_policy is Optional+Computed with a schema
// default, so it is always known here.
func buildJobOptions(data *jobResourceModel) []smplkit.JobOption {
	opts := []smplkit.JobOption{}
	if envs := jobEnvironmentsFromModel(data.Environments); envs != nil {
		opts = append(opts, smplkit.WithJobEnvironments(envs))
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		opts = append(opts, smplkit.WithJobDescription(data.Description.ValueString()))
	}
	if !data.ConcurrencyPolicy.IsNull() && !data.ConcurrencyPolicy.IsUnknown() && data.ConcurrencyPolicy.ValueString() != "" {
		opts = append(opts, smplkit.WithJobConcurrencyPolicy(data.ConcurrencyPolicy.ValueString()))
	}
	return opts
}

// jobEnvironmentsFromModel builds the SDK per-environment override map from
// the Terraform model. A per-environment configuration override carries
// plaintext header values (just like the base configuration); the jobs
// service stores and returns them as supplied, so no redaction handling is
// needed on the round-trip.
func jobEnvironmentsFromModel(envs map[string]jobEnvOverride) map[string]smplkit.JobEnvironment {
	if len(envs) == 0 {
		return nil
	}
	out := make(map[string]smplkit.JobEnvironment, len(envs))
	for env, override := range envs {
		je := smplkit.JobEnvironment{}
		if !override.Enabled.IsNull() && !override.Enabled.IsUnknown() {
			je.Enabled = override.Enabled.ValueBool()
		}
		if override.Configuration != nil {
			cfg := jobHTTPConfigFromModel(override.Configuration)
			je.Configuration = &cfg
		}
		out[env] = je
	}
	return out
}

// jobHTTPConfigFromModel maps the schema's configuration block into the
// SDK's HttpConfig. Fields the user left unset (and which carry no schema
// default) are left zero so the server applies its own defaults; the
// post-Save read then writes the server-authoritative value back to state.
func jobHTTPConfigFromModel(model *jobConfigurationModel) smplkit.HttpConfig {
	if model == nil {
		return smplkit.HttpConfig{}
	}
	out := smplkit.HttpConfig{URL: model.URL.ValueString()}
	if isKnown(model.Method) && model.Method.ValueString() != "" {
		out.Method = smplkit.JobHttpMethod(model.Method.ValueString())
	}
	if isKnown(model.SuccessStatus) && model.SuccessStatus.ValueString() != "" {
		out.SuccessStatus = model.SuccessStatus.ValueString()
	}
	if isKnownInt(model.Timeout) {
		out.Timeout = int(model.Timeout.ValueInt64())
	}
	if isKnown(model.Body) {
		b := model.Body.ValueString()
		out.Body = &b
	}
	if isKnownBool(model.TLSVerify) {
		v := model.TLSVerify.ValueBool()
		out.TlsVerify = &v
	}
	if isKnown(model.CACert) {
		c := model.CACert.ValueString()
		out.CaCert = &c
	}
	if len(model.Headers) > 0 {
		hdrs := make([]smplkit.HttpHeader, 0, len(model.Headers))
		for _, h := range model.Headers {
			hdrs = append(hdrs, smplkit.HttpHeader{Name: h.Name.ValueString(), Value: h.Value.ValueString()})
		}
		out.Headers = hdrs
	}
	return out
}

// applyJobToModel copies a server-authoritative Job into the Terraform
// model.
func applyJobToModel(job *smplkit.Job, model *jobResourceModel) {
	model.ID = types.StringValue(job.ID)
	model.Name = types.StringValue(job.Name)
	model.Description = stringPointerToTypes(job.Description)
	model.Enabled = types.BoolValue(job.Enabled)
	if job.Recurring != nil {
		model.Recurring = types.BoolValue(*job.Recurring)
	} else {
		model.Recurring = types.BoolNull()
	}
	model.Type = types.StringValue(job.Type)
	model.Schedule = types.StringValue(job.Schedule)
	model.ConcurrencyPolicy = types.StringValue(job.ConcurrencyPolicy)
	if len(job.Environments) == 0 {
		model.Environments = nil
	} else {
		envs := make(map[string]jobEnvOverride, len(job.Environments))
		for env, override := range job.Environments {
			eo := jobEnvOverride{Enabled: types.BoolValue(override.Enabled)}
			if override.Configuration != nil {
				eo.Configuration = jobConfigurationModelFromHTTP(*override.Configuration)
			}
			envs[env] = eo
		}
		model.Environments = envs
	}
	model.Configuration = jobConfigurationModelFromHTTP(job.Configuration)
	model.NextRunAt = timePointerToString(job.NextRunAt)
	model.CreatedAt = timePointerToString(job.CreatedAt)
	model.UpdatedAt = timePointerToString(job.UpdatedAt)
	if job.Version != nil {
		model.Version = types.Int64Value(int64(*job.Version))
	} else {
		model.Version = types.Int64Null()
	}
}

// jobConfigurationModelFromHTTP projects a server-returned HttpConfig into
// the Terraform configuration model, applying the same defaults the schema
// documents (method POST, success_status 2xx, timeout 30, tls_verify true)
// so an omitted Optional+Computed field lands on its documented value
// rather than an empty/zero. Header values round-trip plaintext, so they
// are copied verbatim.
func jobConfigurationModelFromHTTP(httpCfg smplkit.HttpConfig) *jobConfigurationModel {
	cfg := &jobConfigurationModel{
		URL:           types.StringValue(httpCfg.URL),
		Method:        types.StringValue(string(httpCfg.Method)),
		SuccessStatus: types.StringValue(httpCfg.SuccessStatus),
		Timeout:       types.Int64Value(int64(httpCfg.Timeout)),
		Body:          types.StringNull(),
		CACert:        types.StringNull(),
	}
	if cfg.Method.ValueString() == "" {
		cfg.Method = types.StringValue("POST")
	}
	if cfg.SuccessStatus.ValueString() == "" {
		cfg.SuccessStatus = types.StringValue("2xx")
	}
	if httpCfg.Timeout == 0 {
		cfg.Timeout = types.Int64Value(30)
	}
	if httpCfg.TlsVerify != nil {
		cfg.TLSVerify = types.BoolValue(*httpCfg.TlsVerify)
	} else {
		cfg.TLSVerify = types.BoolValue(true)
	}
	if httpCfg.Body != nil {
		cfg.Body = types.StringValue(*httpCfg.Body)
	}
	if httpCfg.CaCert != nil {
		cfg.CACert = types.StringValue(*httpCfg.CaCert)
	}
	if len(httpCfg.Headers) > 0 {
		hdrs := make([]jobHeader, 0, len(httpCfg.Headers))
		for _, h := range httpCfg.Headers {
			hdrs = append(hdrs, jobHeader{Name: types.StringValue(h.Name), Value: types.StringValue(h.Value)})
		}
		cfg.Headers = hdrs
	}
	return cfg
}

// isKnown reports whether a types.String carries a usable (non-null,
// non-unknown) value.
func isKnown(v types.String) bool { return !v.IsNull() && !v.IsUnknown() }

// isKnownInt reports whether a types.Int64 carries a usable value.
func isKnownInt(v types.Int64) bool { return !v.IsNull() && !v.IsUnknown() }

// isKnownBool reports whether a types.Bool carries a usable value.
func isKnownBool(v types.Bool) bool { return !v.IsNull() && !v.IsUnknown() }
