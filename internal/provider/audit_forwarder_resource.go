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
	URL           types.String         `tfsdk:"url"`
	Method        types.String         `tfsdk:"method"`
	SuccessStatus types.String         `tfsdk:"success_status"`
	Headers       []forwarderHeader    `tfsdk:"headers"`
}

type forwarderHeader struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

type auditForwarderResourceModel struct {
	ID            types.String                  `tfsdk:"id"`
	Name          types.String                  `tfsdk:"name"`
	Description   types.String                  `tfsdk:"description"`
	ForwarderType types.String                  `tfsdk:"forwarder_type"`
	Enabled       types.Bool                    `tfsdk:"enabled"`
	Filter        types.String                  `tfsdk:"filter"`
	Transform     types.String                  `tfsdk:"transform"`
	TransformType types.String                  `tfsdk:"transform_type"`
	Configuration *forwarderConfigurationModel  `tfsdk:"configuration"`
	CreatedAt     types.String                  `tfsdk:"created_at"`
	UpdatedAt     types.String                  `tfsdk:"updated_at"`
	Version       types.Int64                   `tfsdk:"version"`
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
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether deliveries are attempted. Defaults to `true`.",
				Default:     booldefault.StaticBool(true),
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
			"configuration": schema.SingleNestedAttribute{
				Required:    true,
				Description: "Destination HTTP request configuration. Used for every forwarder type today.",
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
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the forwarder was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server on every write.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"version": schema.Int64Attribute{
				Computed:    true,
				Description: "Monotonic counter bumped by the server on every write.",
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
	// `<redacted>`.
	plannedHeaders := plannedHeadersForState(data.Configuration)
	applyForwarderToModel(fwd, &data)
	if data.Configuration != nil && plannedHeaders != nil {
		data.Configuration.Headers = plannedHeaders
	}

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
	applyForwarderToModel(fwd, &data)
	if data.Configuration != nil && prior != nil {
		// Headers are returned redacted — keep the values we previously
		// wrote to state so we don't decide the user changed every
		// header on every read.
		mergeRedactedHeaders(prior.Headers, data.Configuration.Headers)
	}

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
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		fwd.Enabled = plan.Enabled.ValueBool()
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
	applyForwarderToModel(fwd, &plan)
	if plan.Configuration != nil && plannedHeaders != nil {
		plan.Configuration.Headers = plannedHeaders
	}

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
// `With*` options. Filter, transform, and description are all opt-in.
func buildForwarderOptions(data *auditForwarderResourceModel, diags *diag.Diagnostics) []smplkit.ForwarderOption {
	opts := []smplkit.ForwarderOption{}
	if !data.Enabled.IsNull() && !data.Enabled.IsUnknown() {
		opts = append(opts, smplkit.WithForwarderEnabled(data.Enabled.ValueBool()))
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		opts = append(opts, smplkit.WithForwarderDescription(data.Description.ValueString()))
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
	model.Enabled = types.BoolValue(fwd.Enabled)

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

	cfg := &forwarderConfigurationModel{
		URL:           types.StringValue(fwd.Configuration.URL),
		Method:        types.StringValue(string(fwd.Configuration.Method)),
		SuccessStatus: types.StringValue(fwd.Configuration.SuccessStatus),
	}
	if cfg.Method.ValueString() == "" {
		cfg.Method = types.StringValue("POST")
	}
	if cfg.SuccessStatus.ValueString() == "" {
		cfg.SuccessStatus = types.StringValue("2xx")
	}
	if len(fwd.Configuration.Headers) > 0 {
		hdrs := make([]forwarderHeader, 0, len(fwd.Configuration.Headers))
		for _, h := range fwd.Configuration.Headers {
			hdrs = append(hdrs, forwarderHeader{Name: types.StringValue(h.Name), Value: types.StringValue(h.Value)})
		}
		cfg.Headers = hdrs
	}
	model.Configuration = cfg

	model.CreatedAt = timePointerToString(fwd.CreatedAt)
	model.UpdatedAt = timePointerToString(fwd.UpdatedAt)
	if fwd.Version != nil {
		model.Version = types.Int64Value(int64(*fwd.Version))
	} else {
		model.Version = types.Int64Null()
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
