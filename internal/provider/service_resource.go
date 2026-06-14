package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// NewServiceResource is the factory the provider hands to Terraform for
// the `smplkit_service` resource.
func NewServiceResource() resource.Resource {
	return &serviceResource{}
}

type serviceResource struct {
	client *smplkit.SmplClient
}

type serviceResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	CreatedAt types.String `tfsdk:"created_at"`
	UpdatedAt types.String `tfsdk:"updated_at"`
}

func (r *serviceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (r *serviceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A smplkit service — a backend application or microservice in the customer's stack that contexts " +
			"can be evaluated against. The `id` is caller-supplied and doubles as the import id; updates are full-replace.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The service key (e.g. `user_service`). Caller-supplied at create. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name shown in the smplkit console.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the service was created.",
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

func (r *serviceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*smplkit.SmplClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *smplkit.SmplClient, got %T", req.ProviderData),
		)
		return
	}
	r.client = client
}

func (r *serviceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc := r.client.Platform().Services().New(data.ID.ValueString(), data.Name.ValueString())
	if err := svc.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_service %q", data.ID.ValueString()), err)
		return
	}

	applyServiceToModel(svc, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *serviceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc, err := r.client.Platform().Services().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_service %q", data.ID.ValueString()), err)
		return
	}

	applyServiceToModel(svc, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *serviceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serviceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	svc, err := r.client.Platform().Services().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_service %q before update", state.ID.ValueString()), err)
		return
	}
	svc.Name = plan.Name.ValueString()
	if err := svc.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_service %q", state.ID.ValueString()), err)
		return
	}

	applyServiceToModel(svc, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *serviceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Platform().Services().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_service %q", data.ID.ValueString()), err)
	}
}

func (r *serviceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func applyServiceToModel(svc *smplkit.Service, model *serviceResourceModel) {
	model.ID = types.StringValue(svc.ID)
	model.Name = types.StringValue(svc.Name)
	model.CreatedAt = timePointerToString(svc.CreatedAt)
	model.UpdatedAt = timePointerToString(svc.UpdatedAt)
}
