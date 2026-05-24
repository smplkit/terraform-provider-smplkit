package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// NewEnvironmentResource is the factory the provider hands to Terraform
// for the `smplkit_environment` resource.
func NewEnvironmentResource() resource.Resource {
	return &environmentResource{}
}

type environmentResource struct {
	client *smplkit.ManagementClient
}

type environmentResourceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Color          types.String `tfsdk:"color"`
	Classification types.String `tfsdk:"classification"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

func (r *environmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *environmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A smplkit environment (e.g. `production`, `staging`). Terraform-managed environments land managed " +
			"and STANDARD by default; the create is gated by the account's environment limit. If the gate fires, the " +
			"provider surfaces the server's 402 as an actionable diagnostic naming the limit and the upgrade path.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The environment key (e.g. `production`). Caller-supplied at create. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name shown in the smplkit console.",
			},
			"color": schema.StringAttribute{
				Optional:    true,
				Description: "Optional hex color code (e.g. `#ef4444`) used by the console to tag this environment.",
			},
			"classification": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "`STANDARD` (default, deliberately created and visible in the standard column set) or " +
					"`AD_HOC` (transient — typically discovered by an SDK). Terraform usually wants STANDARD.",
				Validators: []validator.String{
					stringvalidator.OneOf("STANDARD", "AD_HOC"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the environment was created.",
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

func (r *environmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *environmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data environmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := []smplkit.EnvironmentOption{}
	if !data.Color.IsNull() && !data.Color.IsUnknown() && data.Color.ValueString() != "" {
		opts = append(opts, smplkit.WithEnvironmentColor(data.Color.ValueString()))
	}
	if !data.Classification.IsNull() && !data.Classification.IsUnknown() {
		opts = append(opts, smplkit.WithEnvironmentClassification(smplkit.EnvironmentClassification(data.Classification.ValueString())))
	}

	env := r.client.Environments().New(data.ID.ValueString(), data.Name.ValueString(), opts...)
	if err := env.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_environment %q", data.ID.ValueString()), err)
		return
	}

	applyEnvironmentToModel(env, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *environmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data environmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.Environments().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_environment %q", data.ID.ValueString()), err)
		return
	}

	applyEnvironmentToModel(env, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *environmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state environmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := r.client.Environments().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_environment %q before update", state.ID.ValueString()), err)
		return
	}
	env.Name = plan.Name.ValueString()
	if !plan.Color.IsNull() && !plan.Color.IsUnknown() && plan.Color.ValueString() != "" {
		s := plan.Color.ValueString()
		env.Color = &s
	} else {
		env.Color = nil
	}
	if !plan.Classification.IsNull() && !plan.Classification.IsUnknown() {
		env.Classification = smplkit.EnvironmentClassification(plan.Classification.ValueString())
	}
	if err := env.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_environment %q", state.ID.ValueString()), err)
		return
	}

	applyEnvironmentToModel(env, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *environmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data environmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Environments().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_environment %q", data.ID.ValueString()), err)
	}
}

func (r *environmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func applyEnvironmentToModel(env *smplkit.Environment, model *environmentResourceModel) {
	model.ID = types.StringValue(env.ID)
	model.Name = types.StringValue(env.Name)
	model.Color = stringPointerToTypes(env.Color)
	model.Classification = types.StringValue(string(env.Classification))
	model.CreatedAt = timePointerToString(env.CreatedAt)
	model.UpdatedAt = timePointerToString(env.UpdatedAt)
}
