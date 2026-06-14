package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// NewConfigurationResource is the factory the provider hands to
// Terraform for the `smplkit_configuration` resource.
func NewConfigurationResource() resource.Resource {
	return &configurationResource{}
}

type configurationResource struct {
	client *smplkit.SmplClient
}

type configurationResourceModel struct {
	ID           types.String                 `tfsdk:"id"`
	Name         types.String                 `tfsdk:"name"`
	Description  types.String                 `tfsdk:"description"`
	Parent       types.String                 `tfsdk:"parent"`
	Items        map[string]string            `tfsdk:"items"`
	Environments map[string]map[string]string `tfsdk:"environments"`
	CreatedAt    types.String                 `tfsdk:"created_at"`
	UpdatedAt    types.String                 `tfsdk:"updated_at"`
}

func (r *configurationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configuration"
}

func (r *configurationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A smplkit configuration — a named bundle of typed key/value pairs ('items') with optional " +
			"per-environment overrides. Items and overrides are JSON-encoded strings on the Terraform side so that " +
			"any JSON-serializable value (string, number, bool, array, object) can be expressed; use " +
			"`jsonencode(...)` in HCL. Updates are full-replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The configuration key (e.g. `user_service`). Caller-supplied at create. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Display name shown in the smplkit console. Defaults to a humanized form of `id`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional free-text description of the configuration.",
			},
			"parent": schema.StringAttribute{
				Optional:    true,
				Description: "Optional id of a parent configuration to inherit from. Children override parent items.",
			},
			"items": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Map of item key to JSON-encoded value. Use `jsonencode(...)` for non-string values: " +
					"`items = { debug = jsonencode(false), timeout_ms = jsonencode(5000) }`. The server stores the " +
					"decoded JSON; the provider canonicalizes serialization so plans don't churn on key ordering.",
			},
			"environments": schema.MapAttribute{
				Optional:    true,
				ElementType: types.MapType{ElemType: types.StringType},
				Description: "Per-environment item overrides. Keys are environment ids (e.g. `production`); values " +
					"are maps of item key to JSON-encoded override. Items not overridden inherit from `items`.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the configuration was created.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed: true,
				Description: "RFC3339 timestamp set by the server on every write. " +
					"Recomputed on every apply, so plans involving updates show this as `(known after apply)`.",
			},
		},
	}
}

func (r *configurationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *configurationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data configurationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := []smplkit.ConfigOption{}
	if !data.Name.IsNull() && !data.Name.IsUnknown() && data.Name.ValueString() != "" {
		opts = append(opts, smplkit.WithConfigName(data.Name.ValueString()))
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		opts = append(opts, smplkit.WithConfigDescription(data.Description.ValueString()))
	}
	if !data.Parent.IsNull() && !data.Parent.IsUnknown() && data.Parent.ValueString() != "" {
		opts = append(opts, smplkit.WithConfigParent(data.Parent.ValueString()))
	}

	items := stringMapFromJSONStringMap(data.Items, path.Root("items"), &resp.Diagnostics)
	envs := envOverridesFromModel(data.Environments, path.Root("environments"), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	opts = append(opts, smplkit.WithConfigItems(items), smplkit.WithConfigEnvironments(envs))

	cfg := r.client.Config().New(data.ID.ValueString(), opts...)
	if err := cfg.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_configuration %q", data.ID.ValueString()), err)
		return
	}

	applyConfigurationToModel(cfg, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *configurationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data configurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.Config().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_configuration %q", data.ID.ValueString()), err)
		return
	}

	applyConfigurationToModel(cfg, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *configurationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state configurationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, err := r.client.Config().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_configuration %q before update", state.ID.ValueString()), err)
		return
	}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() && plan.Name.ValueString() != "" {
		cfg.Name = plan.Name.ValueString()
	}
	cfg.Description = stringOrNull(plan.Description)
	cfg.Parent = stringOrNull(plan.Parent)
	cfg.Items = stringMapFromJSONStringMap(plan.Items, path.Root("items"), &resp.Diagnostics)
	cfg.Environments = envOverridesFromModel(plan.Environments, path.Root("environments"), &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := cfg.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_configuration %q", state.ID.ValueString()), err)
		return
	}

	applyConfigurationToModel(cfg, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *configurationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data configurationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Config().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_configuration %q", data.ID.ValueString()), err)
	}
}

func (r *configurationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// applyConfigurationToModel writes the SDK's view of a config back into
// the Terraform model, canonicalizing JSON-encoded item values so plans
// stay stable across formatter differences.
func applyConfigurationToModel(cfg *smplkit.ConfigEntry, model *configurationResourceModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(cfg.ID)
	model.Name = types.StringValue(cfg.Name)
	model.Description = stringPointerToTypes(cfg.Description)
	model.Parent = stringPointerToTypes(cfg.Parent)

	items, err := stringMapToJSONStringMap(cfg.Items)
	if err != nil {
		diags.AddError("encoding configuration items for state", err.Error())
		return
	}
	if len(items) == 0 {
		model.Items = nil
	} else {
		model.Items = items
	}

	if len(cfg.Environments) == 0 {
		model.Environments = nil
	} else {
		envOut := make(map[string]map[string]string, len(cfg.Environments))
		for env, overrides := range cfg.Environments {
			encoded, err := stringMapToJSONStringMap(overrides)
			if err != nil {
				diags.AddError(fmt.Sprintf("encoding environment %q overrides for state", env), err.Error())
				return
			}
			if len(encoded) > 0 {
				envOut[env] = encoded
			}
		}
		if len(envOut) == 0 {
			model.Environments = nil
		} else {
			model.Environments = envOut
		}
	}

	model.CreatedAt = timePointerToString(cfg.CreatedAt)
	model.UpdatedAt = timePointerToString(cfg.UpdatedAt)
}

// envOverridesFromModel parses the nested map[env]map[key]json-string
// structure from Terraform state into the SDK's
// map[env]map[key]any shape.
func envOverridesFromModel(envs map[string]map[string]string, attr path.Path, diags *diag.Diagnostics) map[string]map[string]interface{} {
	if len(envs) == 0 {
		return map[string]map[string]interface{}{}
	}
	out := make(map[string]map[string]interface{}, len(envs))
	for env, overrides := range envs {
		out[env] = stringMapFromJSONStringMap(overrides, attr.AtMapKey(env), diags)
	}
	return out
}
