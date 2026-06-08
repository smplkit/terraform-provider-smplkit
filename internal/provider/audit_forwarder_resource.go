package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

// validForwarderTypes mirrors the SDK enum; kept identical to the wire
// strings so casing or naming doesn't drift between schema and SDK.
var validForwarderTypes = []string{"datadog", "elastic", "honeycomb", "http", "new_relic", "splunk_hec", "sumo_logic"}

var validHTTPMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

var validTransformTypes = []string{"JSONATA"}

// NewAuditForwarderResource is the factory the provider hands to
// Terraform for the `smplkit_audit_forwarder` resource.
func NewAuditForwarderResource() resource.Resource {
	return &auditForwarderResource{}
}

type auditForwarderResource struct {
	client *smplkit.ManagementClient
}

type forwarderConfigurationModel struct {
	URL           types.String      `tfsdk:"url"`
	Method        types.String      `tfsdk:"method"`
	SuccessStatus types.String      `tfsdk:"success_status"`
	Headers       []forwarderHeader `tfsdk:"headers"`
}

type forwarderHeader struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

// forwarderEnvOverride is the nested object inside the `environments`
// map. `enabled` toggles whether the forwarder delivers in this
// environment (the base `enabled` is server-pinned false per ADR-055, so
// enablement lives entirely here); `configuration` is an optional
// per-environment HTTP-request override that fully replaces the base
// configuration in that environment — omit it to inherit the base.
type forwarderEnvOverride struct {
	Enabled       types.Bool                   `tfsdk:"enabled"`
	Configuration *forwarderConfigurationModel `tfsdk:"configuration"`
}

type auditForwarderResourceModel struct {
	ID                   types.String                    `tfsdk:"id"`
	Name                 types.String                    `tfsdk:"name"`
	Description          types.String                    `tfsdk:"description"`
	ForwarderType        types.String                    `tfsdk:"forwarder_type"`
	Environments         map[string]forwarderEnvOverride `tfsdk:"environments"`
	ForwardSmplkitEvents types.Bool                      `tfsdk:"forward_smplkit_events"`
	Filter               types.String                    `tfsdk:"filter"`
	Transform            types.String                    `tfsdk:"transform"`
	TransformType        types.String                    `tfsdk:"transform_type"`
	Configuration        *forwarderConfigurationModel    `tfsdk:"configuration"`
	CreatedAt            types.String                    `tfsdk:"created_at"`
	UpdatedAt            types.String                    `tfsdk:"updated_at"`
	Version              types.Int64                     `tfsdk:"version"`
}

func (r *auditForwarderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_audit_forwarder"
}

func (r *auditForwarderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A SIEM-streaming destination for the account's audit events. Available on Pro tier accounts. " +
			"All forwarder types share the same HTTP-request configuration shape today; the `forwarder_type` selects " +
			"the destination family (Splunk HEC, Datadog Logs, generic HTTP, etc.) and shapes the outbound payload " +
			"envelope server-side.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The forwarder key (e.g. `splunk-prod`). Caller-supplied at create. " +
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
			"forwarder_type": schema.StringAttribute{
				Required: true,
				Description: "Destination family. One of `datadog`, `elastic`, `honeycomb`, `http`, `new_relic`, " +
					"`splunk_hec`, `sumo_logic`. Controls how the audit service shapes each outbound payload.",
				Validators: []validator.String{stringvalidator.OneOf(validForwarderTypes...)},
			},
			"environments": schema.MapNestedAttribute{
				Optional: true,
				Description: "Per-environment configuration keyed by environment id (e.g. `production`). " +
					"A forwarder delivers events in an environment only when that environment's entry sets " +
					"`enabled = true`; an environment with no entry receives no deliveries. Each entry may also " +
					"carry a `configuration` override that fully replaces the base `configuration` in that environment. " +
					"Every referenced environment must already exist and be managed for the account.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether the forwarder delivers events in this environment. Defaults to `false`.",
						},
						"configuration": forwarderConfigurationSchemaAttribute(false,
							"Optional per-environment HTTP request configuration that fully replaces the base "+
								"`configuration` in this environment. Omit to inherit the base configuration."),
					},
				},
			},
			"forward_smplkit_events": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				Description: "When `true`, this forwarder also receives platform change events that smplkit records " +
					"about your own resources (flag, configuration, and similar changes). Each such event is delivered " +
					"through every environment this forwarder is enabled in, using that environment's resolved " +
					"configuration. Defaults to `false` — platform change events are not forwarded unless you opt in. " +
					"Independent of the per-environment `enabled` settings, since platform change events are not tied " +
					"to a deployment environment.",
			},
			"filter": schema.StringAttribute{
				Optional: true,
				Description: "Optional JSON Logic expression (as a JSON-encoded string) evaluated against each event. " +
					"Events that don't match are recorded as filtered-out deliveries and not forwarded.",
			},
			"transform": schema.StringAttribute{
				Optional: true,
				Description: "Optional template applied to each event before delivery. Shape depends on `transform_type`; " +
					"for `JSONATA` this is the JSONata expression string. Must be set together with `transform_type`.",
			},
			"transform_type": schema.StringAttribute{
				Optional:    true,
				Description: "Engine used to evaluate `transform`. Currently only `JSONATA` is supported.",
				Validators:  []validator.String{stringvalidator.OneOf(validTransformTypes...)},
			},
			"configuration": forwarderConfigurationSchemaAttribute(true,
				"Destination HTTP request configuration. Used for every forwarder type today."),
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the forwarder was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
				Description: "RFC3339 timestamp set by the server on every write. " +
					"Recomputed on every apply, so plans involving updates show this as `(known after apply)`.",
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic counter bumped by the server on every write.",
			},
		},
	}
}

// forwarderConfigurationSchemaAttribute builds the SingleNestedAttribute
// used for both the forwarder's base `configuration` and each
// per-environment override `configuration`. required controls whether the
// block must be supplied (true for the base configuration, false for the
// optional per-environment override); description is the block's
// top-level docstring.
func forwarderConfigurationSchemaAttribute(required bool, description string) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Required:    required,
		Optional:    !required,
		Description: description,
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Destination URL the audit service POSTs each event to.",
			},
			"method": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "HTTP method. Defaults to POST.",
				Validators:  []validator.String{stringvalidator.OneOf(validHTTPMethods...)},
			},
			"success_status": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Status the destination must return for delivery to count as success. Exact code " +
					"(`200`) or class (`2xx`). Defaults to `2xx`.",
			},
			"headers": schema.ListNestedAttribute{
				Optional: true,
				Description: "Headers attached to every outbound request. Values are encrypted at rest server-side; " +
					"reads return them as `<redacted>` so the provider keeps the planned value in state to avoid " +
					"spurious diffs.",
				Validators: []validator.List{listvalidator.SizeAtLeast(0)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Header name.",
						},
						"value": schema.StringAttribute{
							Required:    true,
							Sensitive:   true,
							Description: "Header value (e.g. an auth token). Sensitive — stored encrypted server-side and redacted on reads.",
						},
					},
				},
			},
		},
	}
}

func (r *auditForwarderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	r.client = client
}

func (r *auditForwarderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data auditForwarderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configuration := httpConfigurationFromModel(data.Configuration)
	opts := buildForwarderOptions(&data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	fwd := r.client.Audit().Forwarders().New(
		data.ID.ValueString(),
		data.Name.ValueString(),
		smplkit.ForwarderType(data.ForwarderType.ValueString()),
		configuration,
		opts...,
	)
	if err := fwd.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_audit_forwarder %q", data.ID.ValueString()), err)
		return
	}

	// Headers come back redacted from the server. Preserve the plan's
	// header values in state so subsequent plans don't diff against
	// `<redacted>` — for both the base configuration and every
	// per-environment configuration override.
	plannedHeaders := plannedHeadersForState(data.Configuration)
	plannedEnvs := data.Environments
	applyForwarderToModel(fwd, &data)
	if data.Configuration != nil && plannedHeaders != nil {
		data.Configuration.Headers = plannedHeaders
	}
	restorePlannedEnvHeaders(plannedEnvs, data.Environments)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *auditForwarderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data auditForwarderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fwd, err := r.client.Audit().Forwarders().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_audit_forwarder %q", data.ID.ValueString()), err)
		return
	}

	prior := data.Configuration
	priorEnvs := data.Environments
	applyForwarderToModel(fwd, &data)
	if data.Configuration != nil && prior != nil {
		// Headers are returned redacted — keep the values we previously
		// wrote to state so we don't decide the user changed every
		// header on every read.
		mergeRedactedHeaders(prior.Headers, data.Configuration.Headers)
	}
	mergeRedactedEnvHeaders(priorEnvs, data.Environments)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *auditForwarderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state auditForwarderResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fwd, err := r.client.Audit().Forwarders().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_audit_forwarder %q before update", state.ID.ValueString()), err)
		return
	}

	fwd.Name = plan.Name.ValueString()
	fwd.Description = stringOrNull(plan.Description)
	fwd.ForwarderType = smplkit.ForwarderType(plan.ForwarderType.ValueString())
	// Full-replace the per-environment override map with the plan's. A
	// nil map clears enablement everywhere (the base `enabled` is
	// server-pinned false per ADR-055).
	fwd.Environments = forwarderEnvironmentsFromModel(plan.Environments)
	if !plan.ForwardSmplkitEvents.IsNull() && !plan.ForwardSmplkitEvents.IsUnknown() {
		v := plan.ForwardSmplkitEvents.ValueBool()
		fwd.ForwardSmplkitEvents = &v
	} else {
		fwd.ForwardSmplkitEvents = nil
	}
	if !plan.Filter.IsNull() && !plan.Filter.IsUnknown() && plan.Filter.ValueString() != "" {
		parsed, err := parseJSONString(plan.Filter.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("filter"), "Invalid JSON Logic", err.Error())
			return
		}
		if m, ok := parsed.(map[string]interface{}); ok {
			fwd.Filter = m
		} else {
			resp.Diagnostics.AddAttributeError(path.Root("filter"), "Invalid JSON Logic", "filter must be a JSON object")
			return
		}
	} else {
		fwd.Filter = nil
	}
	if !plan.Transform.IsNull() && !plan.Transform.IsUnknown() && plan.Transform.ValueString() != "" {
		fwd.Transform = plan.Transform.ValueString()
		if !plan.TransformType.IsNull() && !plan.TransformType.IsUnknown() {
			tt := smplkit.ForwarderTransformType(plan.TransformType.ValueString())
			fwd.TransformType = &tt
		}
	} else {
		fwd.Transform = nil
		fwd.TransformType = nil
	}
	fwd.Configuration = httpConfigurationFromModel(plan.Configuration)

	if err := fwd.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_audit_forwarder %q", state.ID.ValueString()), err)
		return
	}

	plannedHeaders := plannedHeadersForState(plan.Configuration)
	plannedEnvs := plan.Environments
	applyForwarderToModel(fwd, &plan)
	if plan.Configuration != nil && plannedHeaders != nil {
		plan.Configuration.Headers = plannedHeaders
	}
	restorePlannedEnvHeaders(plannedEnvs, plan.Environments)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *auditForwarderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data auditForwarderResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Audit().Forwarders().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_audit_forwarder %q", data.ID.ValueString()), err)
	}
}

func (r *auditForwarderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// buildForwarderOptions threads optional attributes through the SDK's
// `With*` options. Environments, filter, transform, and description are
// all opt-in.
func buildForwarderOptions(data *auditForwarderResourceModel, diags *diag.Diagnostics) []smplkit.ForwarderOption {
	opts := []smplkit.ForwarderOption{}
	if envs := forwarderEnvironmentsFromModel(data.Environments); envs != nil {
		opts = append(opts, smplkit.WithForwarderEnvironments(envs))
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		opts = append(opts, smplkit.WithForwarderDescription(data.Description.ValueString()))
	}
	if !data.ForwardSmplkitEvents.IsNull() && !data.ForwardSmplkitEvents.IsUnknown() {
		opts = append(opts, smplkit.WithForwardSmplkitEvents(data.ForwardSmplkitEvents.ValueBool()))
	}
	if !data.Filter.IsNull() && !data.Filter.IsUnknown() && data.Filter.ValueString() != "" {
		parsed, err := parseJSONString(data.Filter.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("filter"), "Invalid JSON Logic", err.Error())
			return opts
		}
		m, ok := parsed.(map[string]interface{})
		if !ok {
			diags.AddAttributeError(path.Root("filter"), "Invalid JSON Logic", "filter must be a JSON object")
			return opts
		}
		opts = append(opts, smplkit.WithForwarderFilter(m))
	}
	if !data.Transform.IsNull() && !data.Transform.IsUnknown() && data.Transform.ValueString() != "" {
		ttStr := "JSONATA"
		if !data.TransformType.IsNull() && !data.TransformType.IsUnknown() {
			ttStr = data.TransformType.ValueString()
		}
		opts = append(opts, smplkit.WithForwarderTransform(smplkit.ForwarderTransformType(ttStr), data.Transform.ValueString()))
	}
	return opts
}

// httpConfigurationFromModel maps the schema's configuration block into
// the SDK's HttpConfiguration shape, defaulting method to POST so the
// SDK and server don't disagree.
func httpConfigurationFromModel(model *forwarderConfigurationModel) smplkit.HttpConfiguration {
	if model == nil {
		return smplkit.HttpConfiguration{}
	}
	out := smplkit.HttpConfiguration{URL: model.URL.ValueString()}
	if !model.Method.IsNull() && !model.Method.IsUnknown() && model.Method.ValueString() != "" {
		out.Method = smplkit.HttpMethod(model.Method.ValueString())
	}
	if !model.SuccessStatus.IsNull() && !model.SuccessStatus.IsUnknown() && model.SuccessStatus.ValueString() != "" {
		out.SuccessStatus = model.SuccessStatus.ValueString()
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

func applyForwarderToModel(fwd *smplkit.Forwarder, model *auditForwarderResourceModel) {
	model.ID = types.StringValue(fwd.ID)
	model.Name = types.StringValue(fwd.Name)
	model.Description = stringPointerToTypes(fwd.Description)
	model.ForwarderType = types.StringValue(string(fwd.ForwarderType))

	// forward_smplkit_events is Optional+Computed with a server-side
	// default of false, so a nil from the API maps to the documented
	// default rather than a null that would perturb the plan.
	if fwd.ForwardSmplkitEvents != nil {
		model.ForwardSmplkitEvents = types.BoolValue(*fwd.ForwardSmplkitEvents)
	} else {
		model.ForwardSmplkitEvents = types.BoolValue(false)
	}

	if fwd.Filter == nil {
		model.Filter = types.StringNull()
	} else {
		s, err := marshalCanonical(fwd.Filter)
		if err == nil {
			model.Filter = types.StringValue(s)
		}
	}
	if fwd.Transform == nil {
		model.Transform = types.StringNull()
	} else if s, ok := fwd.Transform.(string); ok {
		model.Transform = types.StringValue(s)
	} else if s, err := marshalCanonical(fwd.Transform); err == nil {
		model.Transform = types.StringValue(s)
	}
	if fwd.TransformType == nil {
		model.TransformType = types.StringNull()
	} else {
		model.TransformType = types.StringValue(string(*fwd.TransformType))
	}

	model.Configuration = configurationModelFromHTTP(fwd.Configuration)

	if len(fwd.Environments) == 0 {
		model.Environments = nil
	} else {
		envs := make(map[string]forwarderEnvOverride, len(fwd.Environments))
		for env, override := range fwd.Environments {
			eo := forwarderEnvOverride{Enabled: types.BoolValue(override.Enabled)}
			if override.Configuration != nil {
				eo.Configuration = configurationModelFromHTTP(*override.Configuration)
			}
			envs[env] = eo
		}
		model.Environments = envs
	}

	model.CreatedAt = timePointerToString(fwd.CreatedAt)
	model.UpdatedAt = timePointerToString(fwd.UpdatedAt)
	if fwd.Version != nil {
		model.Version = types.Int64Value(int64(*fwd.Version))
	} else {
		model.Version = types.Int64Null()
	}
}

// configurationModelFromHTTP projects a server-returned HttpConfiguration
// into the Terraform configuration model, applying the same method/status
// defaults the schema documents. Header values arrive redacted — callers
// restore the plan/prior values via plannedHeadersForState /
// mergeRedactedHeaders.
func configurationModelFromHTTP(httpCfg smplkit.HttpConfiguration) *forwarderConfigurationModel {
	cfg := &forwarderConfigurationModel{
		URL:           types.StringValue(httpCfg.URL),
		Method:        types.StringValue(string(httpCfg.Method)),
		SuccessStatus: types.StringValue(httpCfg.SuccessStatus),
	}
	if cfg.Method.ValueString() == "" {
		cfg.Method = types.StringValue("POST")
	}
	if cfg.SuccessStatus.ValueString() == "" {
		cfg.SuccessStatus = types.StringValue("2xx")
	}
	if len(httpCfg.Headers) > 0 {
		hdrs := make([]forwarderHeader, 0, len(httpCfg.Headers))
		for _, h := range httpCfg.Headers {
			hdrs = append(hdrs, forwarderHeader{Name: types.StringValue(h.Name), Value: types.StringValue(h.Value)})
		}
		cfg.Headers = hdrs
	}
	return cfg
}

// forwarderEnvironmentsFromModel builds the SDK per-environment override
// map from the Terraform model. A per-environment configuration override
// carries plaintext header values (just like the base configuration); the
// server redacts them on reads, and the post-Save restore in Create /
// Update keeps the plan's plaintext values in state.
func forwarderEnvironmentsFromModel(envs map[string]forwarderEnvOverride) map[string]smplkit.ForwarderEnvironment {
	if len(envs) == 0 {
		return nil
	}
	out := make(map[string]smplkit.ForwarderEnvironment, len(envs))
	for env, override := range envs {
		fe := smplkit.ForwarderEnvironment{}
		if !override.Enabled.IsNull() && !override.Enabled.IsUnknown() {
			fe.Enabled = override.Enabled.ValueBool()
		}
		if override.Configuration != nil {
			cfg := httpConfigurationFromModel(override.Configuration)
			fe.Configuration = &cfg
		}
		out[env] = fe
	}
	return out
}

// restorePlannedEnvHeaders re-stamps the plan's plaintext per-environment
// header values onto the post-Save model. The server returns those header
// values redacted, so without this restore the state would persist
// "<redacted>" for every per-environment override header. planned is the
// plan/config the user supplied (the source of truth for plaintext
// values); model is the server-applied result being written to state.
func restorePlannedEnvHeaders(planned, model map[string]forwarderEnvOverride) {
	for env, planOverride := range planned {
		if planOverride.Configuration == nil {
			continue
		}
		modelOverride, ok := model[env]
		if !ok || modelOverride.Configuration == nil {
			continue
		}
		if headers := plannedHeadersForState(planOverride.Configuration); headers != nil {
			modelOverride.Configuration.Headers = headers
			model[env] = modelOverride
		}
	}
}

// mergeRedactedEnvHeaders restores redacted per-environment header values
// from prior state on Read, mirroring mergeRedactedHeaders for the base
// configuration.
func mergeRedactedEnvHeaders(prior, current map[string]forwarderEnvOverride) {
	for env, currentOverride := range current {
		if currentOverride.Configuration == nil {
			continue
		}
		priorOverride, ok := prior[env]
		if !ok || priorOverride.Configuration == nil {
			continue
		}
		mergeRedactedHeaders(priorOverride.Configuration.Headers, currentOverride.Configuration.Headers)
	}
}

// plannedHeadersForState clones the planned headers verbatim so we can
// restore them after the server response (which redacts values).
func plannedHeadersForState(cfg *forwarderConfigurationModel) []forwarderHeader {
	if cfg == nil || len(cfg.Headers) == 0 {
		return nil
	}
	out := make([]forwarderHeader, len(cfg.Headers))
	copy(out, cfg.Headers)
	return out
}

// mergeRedactedHeaders replaces redacted header values returned by the
// server with whatever the prior Terraform state held for the same name.
// Without this Read would constantly report a change from the real
// value to "<redacted>".
func mergeRedactedHeaders(prior, current []forwarderHeader) {
	if len(current) == 0 || len(prior) == 0 {
		return
	}
	priorIdx := make(map[string]string, len(prior))
	for _, p := range prior {
		priorIdx[p.Name.ValueString()] = p.Value.ValueString()
	}
	for i, h := range current {
		if h.Value.ValueString() == "<redacted>" {
			if v, ok := priorIdx[h.Name.ValueString()]; ok && v != "" {
				current[i].Value = types.StringValue(v)
			}
		}
	}
}
