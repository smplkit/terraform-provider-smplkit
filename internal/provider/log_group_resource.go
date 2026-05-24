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

// validLogLevels enumerates the wire-form values the API accepts. Kept
// here so the same set drives both the resource's schema validator and
// the data source.
var validLogLevels = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL", "SILENT"}

// NewLogGroupResource is the factory the provider hands to Terraform for
// the `smplkit_log_group` resource.
func NewLogGroupResource() resource.Resource {
	return &logGroupResource{}
}

type logGroupResource struct {
	client *smplkit.ManagementClient
}

type logGroupResourceModel struct {
	ID           types.String      `tfsdk:"id"`
	Name         types.String      `tfsdk:"name"`
	Level        types.String      `tfsdk:"level"`
	ParentGroup  types.String      `tfsdk:"parent_group"`
	Environments map[string]string `tfsdk:"environments"`
	CreatedAt    types.String      `tfsdk:"created_at"`
	UpdatedAt    types.String      `tfsdk:"updated_at"`
}

func (r *logGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_group"
}

func (r *logGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A smplkit log group. Groups bundle related loggers (e.g. `http`, `database`) so admins can " +
			"set a base level once and have every member logger inherit. Per-environment level overrides apply on " +
			"top of the base level. Groups can nest via `parent_group`. Member loggers are managed by their owning " +
			"runtime SDKs and not modeled here.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The log group key (e.g. `http`). Caller-supplied at create. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Display name shown in the smplkit console. Defaults to a humanized form of `id`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"level": schema.StringAttribute{
				Optional: true,
				Description: "Optional base log level that applies in every environment unless overridden. " +
					"One of TRACE, DEBUG, INFO, WARN, ERROR, FATAL, SILENT. Omit to inherit.",
				Validators: []validator.String{stringvalidator.OneOf(validLogLevels...)},
			},
			"parent_group": schema.StringAttribute{
				Optional: true,
				Description: "Optional parent log group id. Groups nest — a child group inherits the parent's level " +
					"unless it sets its own.",
			},
			"environments": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Map of environment key to log level for per-environment overrides. " +
					"Each value must be one of TRACE, DEBUG, INFO, WARN, ERROR, FATAL, SILENT.",
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the log group was created.",
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

func (r *logGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *logGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data logGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	opts := []smplkit.LogGroupOption{}
	if !data.Name.IsNull() && !data.Name.IsUnknown() && data.Name.ValueString() != "" {
		opts = append(opts, smplkit.WithLogGroupName(data.Name.ValueString()))
	}
	if !data.ParentGroup.IsNull() && !data.ParentGroup.IsUnknown() && data.ParentGroup.ValueString() != "" {
		opts = append(opts, smplkit.WithLogGroupParent(data.ParentGroup.ValueString()))
	}

	group := r.client.LogGroups().New(data.ID.ValueString(), opts...)
	if !data.Level.IsNull() && !data.Level.IsUnknown() && data.Level.ValueString() != "" {
		group.SetLevel(smplkit.LogLevel(data.Level.ValueString()))
	}
	for env, level := range data.Environments {
		if err := validateLogLevel(level); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("environments").AtMapKey(env), "Invalid log level", err.Error())
			continue
		}
		group.SetEnvironmentLevel(env, smplkit.LogLevel(level))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if err := group.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_log_group %q", data.ID.ValueString()), err)
		return
	}

	applyLogGroupToModel(group, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *logGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data logGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.LogGroups().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_log_group %q", data.ID.ValueString()), err)
		return
	}

	applyLogGroupToModel(group, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *logGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state logGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := r.client.LogGroups().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_log_group %q before update", state.ID.ValueString()), err)
		return
	}
	if !plan.Name.IsNull() && !plan.Name.IsUnknown() && plan.Name.ValueString() != "" {
		group.Name = plan.Name.ValueString()
	}
	if !plan.Level.IsNull() && !plan.Level.IsUnknown() && plan.Level.ValueString() != "" {
		group.SetLevel(smplkit.LogLevel(plan.Level.ValueString()))
	} else {
		group.ClearLevel()
	}
	if !plan.ParentGroup.IsNull() && !plan.ParentGroup.IsUnknown() && plan.ParentGroup.ValueString() != "" {
		s := plan.ParentGroup.ValueString()
		group.Group = &s
	} else {
		group.Group = nil
	}
	group.ClearAllEnvironmentLevels()
	for env, level := range plan.Environments {
		if err := validateLogLevel(level); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("environments").AtMapKey(env), "Invalid log level", err.Error())
			continue
		}
		group.SetEnvironmentLevel(env, smplkit.LogLevel(level))
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if err := group.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_log_group %q", state.ID.ValueString()), err)
		return
	}

	applyLogGroupToModel(group, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *logGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data logGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.LogGroups().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_log_group %q", data.ID.ValueString()), err)
	}
}

func (r *logGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func applyLogGroupToModel(group *smplkit.LogGroup, model *logGroupResourceModel) {
	model.ID = types.StringValue(group.ID)
	model.Name = types.StringValue(group.Name)
	if group.Level != nil {
		model.Level = types.StringValue(string(*group.Level))
	} else {
		model.Level = types.StringNull()
	}
	model.ParentGroup = stringPointerToTypes(group.Group)
	model.Environments = logGroupEnvironmentsToMap(group.Environments)
	model.CreatedAt = timePointerToString(group.CreatedAt)
	model.UpdatedAt = timePointerToString(group.UpdatedAt)
}

// logGroupEnvironmentsToMap projects the SDK's
// `map[envName] -> {"level": "INFO"}` shape down to the flat
// `map[envName] -> "INFO"` that the Terraform schema exposes.
func logGroupEnvironmentsToMap(envs map[string]interface{}) map[string]string {
	if len(envs) == 0 {
		return nil
	}
	out := make(map[string]string, len(envs))
	for env, raw := range envs {
		inner, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		level, ok := inner["level"].(string)
		if !ok || level == "" {
			continue
		}
		out[env] = level
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validateLogLevel(level string) error {
	for _, allowed := range validLogLevels {
		if level == allowed {
			return nil
		}
	}
	return fmt.Errorf("log level %q is not one of %v", level, validLogLevels)
}
