package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// All six data sources follow the same shape: take an `id`, look the
// resource up via the management client, copy the SDK model back into
// the matching resource model, return. The resource models declared
// elsewhere in this package are reused so a single mapping function
// services both sides.

// ───── smplkit_service ─────────────────────────────────────────────────────

// NewServiceDataSource returns the read-only data source for services.
func NewServiceDataSource() datasource.DataSource {
	return &serviceDataSource{}
}

type serviceDataSource struct {
	client *smplkit.ManagementClient
}

func (d *serviceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (d *serviceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit service by id.",
		Attributes: map[string]dsschema.Attribute{
			"id":         dsschema.StringAttribute{Required: true, Description: "The service key."},
			"name":       dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"created_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
		},
	}
}

func (d *serviceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *serviceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data serviceResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	svc, err := d.client.Services().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_service data source %q", data.ID.ValueString()), err)
		return
	}
	applyServiceToModel(svc, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ───── smplkit_environment ─────────────────────────────────────────────────

// NewEnvironmentDataSource returns the read-only data source for environments.
func NewEnvironmentDataSource() datasource.DataSource {
	return &environmentDataSource{}
}

type environmentDataSource struct {
	client *smplkit.ManagementClient
}

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit environment by id.",
		Attributes: map[string]dsschema.Attribute{
			"id":             dsschema.StringAttribute{Required: true, Description: "The environment key."},
			"name":           dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"color":          dsschema.StringAttribute{Computed: true, Description: "Hex color string used in the console UI."},
			"classification": dsschema.StringAttribute{Computed: true, Description: "`STANDARD` or `AD_HOC`."},
			"created_at":     dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at":     dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
		},
	}
}

func (d *environmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *environmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data environmentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	env, err := d.client.Environments().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_environment data source %q", data.ID.ValueString()), err)
		return
	}
	applyEnvironmentToModel(env, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ───── smplkit_log_group ───────────────────────────────────────────────────

// NewLogGroupDataSource returns the read-only data source for log groups.
func NewLogGroupDataSource() datasource.DataSource {
	return &logGroupDataSource{}
}

type logGroupDataSource struct {
	client *smplkit.ManagementClient
}

func (d *logGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_log_group"
}

func (d *logGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit log group by id.",
		Attributes: map[string]dsschema.Attribute{
			"id":           dsschema.StringAttribute{Required: true, Description: "The log group key."},
			"name":         dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"level":        dsschema.StringAttribute{Computed: true, Description: "Base log level (TRACE…SILENT)."},
			"parent_group": dsschema.StringAttribute{Computed: true, Description: "Parent log group id."},
			"environments": dsschema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Per-environment log level overrides.",
			},
			"created_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
		},
	}
}

func (d *logGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *logGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data logGroupResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	group, err := d.client.LogGroups().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_log_group data source %q", data.ID.ValueString()), err)
		return
	}
	applyLogGroupToModel(group, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ───── smplkit_configuration ───────────────────────────────────────────────

// NewConfigurationDataSource returns the read-only data source for configurations.
func NewConfigurationDataSource() datasource.DataSource {
	return &configurationDataSource{}
}

type configurationDataSource struct {
	client *smplkit.ManagementClient
}

func (d *configurationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_configuration"
}

func (d *configurationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit configuration by id. Items and per-environment overrides are returned as JSON-encoded strings.",
		Attributes: map[string]dsschema.Attribute{
			"id":          dsschema.StringAttribute{Required: true, Description: "The configuration key."},
			"name":        dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"description": dsschema.StringAttribute{Computed: true, Description: "Optional description."},
			"parent":      dsschema.StringAttribute{Computed: true, Description: "Parent configuration id."},
			"items": dsschema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Base configuration values, each JSON-encoded.",
			},
			"environments": dsschema.MapAttribute{
				Computed:    true,
				ElementType: types.MapType{ElemType: types.StringType},
				Description: "Per-environment item overrides, each JSON-encoded.",
			},
			"created_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
		},
	}
}

func (d *configurationDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *configurationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data configurationResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cfg, err := d.client.Config().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_configuration data source %q", data.ID.ValueString()), err)
		return
	}
	applyConfigurationToModel(cfg, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ───── smplkit_flag ────────────────────────────────────────────────────────

// NewFlagDataSource returns the read-only data source for flags.
func NewFlagDataSource() datasource.DataSource {
	return &flagDataSource{}
}

type flagDataSource struct {
	client *smplkit.ManagementClient
}

func (d *flagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flag"
}

func (d *flagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit flag by id. `default`, value `value` entries, and rule `logic`/`value` are " +
			"returned as JSON-encoded strings.",
		Attributes: map[string]dsschema.Attribute{
			"id":          dsschema.StringAttribute{Required: true, Description: "The flag key."},
			"name":        dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"type":        dsschema.StringAttribute{Computed: true, Description: "Flag type: BOOLEAN, STRING, NUMERIC, or JSON."},
			"description": dsschema.StringAttribute{Computed: true, Description: "Optional description."},
			"default":     dsschema.StringAttribute{Computed: true, Description: "JSON-encoded default value."},
			"values": dsschema.ListNestedAttribute{
				Computed:    true,
				Description: "Optional constrained value set.",
				NestedObject: dsschema.NestedAttributeObject{
					Attributes: map[string]dsschema.Attribute{
						"name":  dsschema.StringAttribute{Computed: true, Description: "Display label."},
						"value": dsschema.StringAttribute{Computed: true, Description: "JSON-encoded value."},
					},
				},
			},
			"environments": dsschema.MapNestedAttribute{
				Computed:    true,
				Description: "Per-environment overrides.",
				NestedObject: dsschema.NestedAttributeObject{
					Attributes: map[string]dsschema.Attribute{
						"enabled": dsschema.BoolAttribute{Computed: true, Description: "Whether the flag is evaluated in this environment."},
						"default": dsschema.StringAttribute{Computed: true, Description: "Environment-specific default (JSON-encoded)."},
						"rules": dsschema.ListNestedAttribute{
							Computed:    true,
							Description: "Ordered list of JSON Logic targeting rules.",
							NestedObject: dsschema.NestedAttributeObject{
								Attributes: map[string]dsschema.Attribute{
									"description": dsschema.StringAttribute{Computed: true, Description: "Human-readable description."},
									"logic":       dsschema.StringAttribute{Computed: true, Description: "JSON Logic expression (JSON-encoded)."},
									"value":       dsschema.StringAttribute{Computed: true, Description: "Value served on match (JSON-encoded)."},
								},
							},
						},
					},
				},
			},
			"created_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at": dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
		},
	}
}

func (d *flagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *flagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data flagResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := d.client.Flags().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_flag data source %q", data.ID.ValueString()), err)
		return
	}
	applyFlagToModel(f, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// ───── smplkit_audit_forwarder ─────────────────────────────────────────────

// forwarderConfigurationDataSourceAttribute builds the read-only nested
// configuration block shared by the forwarder data source's base
// `configuration` and each per-environment override `configuration`.
func forwarderConfigurationDataSourceAttribute(description string) dsschema.SingleNestedAttribute {
	return dsschema.SingleNestedAttribute{
		Computed:    true,
		Description: description,
		Attributes: map[string]dsschema.Attribute{
			"url":            dsschema.StringAttribute{Computed: true, Description: "Destination URL."},
			"method":         dsschema.StringAttribute{Computed: true, Description: "HTTP method."},
			"success_status": dsschema.StringAttribute{Computed: true, Description: "Required success status."},
			"headers": dsschema.ListNestedAttribute{
				Computed:    true,
				Description: "Headers attached to every outbound request (values redacted).",
				NestedObject: dsschema.NestedAttributeObject{
					Attributes: map[string]dsschema.Attribute{
						"name":  dsschema.StringAttribute{Computed: true, Description: "Header name."},
						"value": dsschema.StringAttribute{Computed: true, Sensitive: true, Description: "Header value (always `<redacted>`)."},
					},
				},
			},
		},
	}
}

// NewAuditForwarderDataSource returns the read-only data source for forwarders.
func NewAuditForwarderDataSource() datasource.DataSource {
	return &auditForwarderDataSource{}
}

type auditForwarderDataSource struct {
	client *smplkit.ManagementClient
}

func (d *auditForwarderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_audit_forwarder"
}

func (d *auditForwarderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dsschema.Schema{
		Description: "Read a smplkit audit forwarder by id. Header values are returned as `<redacted>`; the real " +
			"values live only on the audit service side.",
		Attributes: map[string]dsschema.Attribute{
			"id":             dsschema.StringAttribute{Required: true, Description: "The forwarder key."},
			"name":           dsschema.StringAttribute{Computed: true, Description: "Display name."},
			"description":    dsschema.StringAttribute{Computed: true, Description: "Optional description."},
			"forwarder_type": dsschema.StringAttribute{Computed: true, Description: "Destination family."},
			"environments": dsschema.MapNestedAttribute{
				Computed: true,
				Description: "Per-environment configuration keyed by environment id. The forwarder delivers events " +
					"in an environment only when that environment's entry is `enabled`.",
				NestedObject: dsschema.NestedAttributeObject{
					Attributes: map[string]dsschema.Attribute{
						"enabled":       dsschema.BoolAttribute{Computed: true, Description: "Whether the forwarder delivers events in this environment."},
						"configuration": forwarderConfigurationDataSourceAttribute("Per-environment HTTP request configuration override. Header values are redacted."),
					},
				},
			},
			"forward_smplkit_events": dsschema.BoolAttribute{
				Computed: true,
				Description: "Whether this forwarder also receives platform change events that smplkit records about " +
					"your own resources (flag, configuration, and similar changes). When `true`, each such event is " +
					"delivered through every environment this forwarder is enabled in. Independent of the per-environment " +
					"`enabled` settings.",
			},
			"filter":         dsschema.StringAttribute{Computed: true, Description: "JSON Logic filter (JSON-encoded)."},
			"transform":      dsschema.StringAttribute{Computed: true, Description: "Transform expression."},
			"transform_type": dsschema.StringAttribute{Computed: true, Description: "Transform engine (currently JSONATA)."},
			"configuration":  forwarderConfigurationDataSourceAttribute("Destination HTTP request configuration. Header values are redacted."),
			"created_at":     dsschema.StringAttribute{Computed: true, Description: "RFC3339 creation timestamp."},
			"updated_at":     dsschema.StringAttribute{Computed: true, Description: "RFC3339 last-modified timestamp."},
			"version":        dsschema.Int64Attribute{Computed: true, Description: "Monotonic version counter."},
		},
	}
}

func (d *auditForwarderDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*smplkit.ManagementClient)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *smplkit.ManagementClient, got %T", req.ProviderData))
		return
	}
	d.client = c
}

func (d *auditForwarderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data auditForwarderResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	fwd, err := d.client.Audit().Forwarders().Get(ctx, data.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_audit_forwarder data source %q", data.ID.ValueString()), err)
		return
	}
	applyForwarderToModel(fwd, &data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
