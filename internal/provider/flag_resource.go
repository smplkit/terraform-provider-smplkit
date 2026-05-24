package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

// validFlagTypes mirrors the SDK enum so the schema validator and the
// data source share one source of truth.
var validFlagTypes = []string{"BOOLEAN", "STRING", "NUMERIC", "JSON"}

// NewFlagResource is the factory the provider hands to Terraform for the
// `smplkit_flag` resource.
func NewFlagResource() resource.Resource {
	return &flagResource{}
}

type flagResource struct {
	client *smplkit.ManagementClient
}

// flagEnvOverride is the nested object inside the `environments` map.
// `enabled` toggles whether the flag is even evaluated in this
// environment; `default` is the env-specific fallback value; `rules`
// holds an ordered list of JSON Logic targeting rules.
type flagEnvOverride struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Default types.String `tfsdk:"default"`
	Rules   []flagRule   `tfsdk:"rules"`
}

// flagRule mirrors a single targeting-rule entry. `logic` and `value`
// are JSON-encoded strings so any JSON Logic expression and any value
// type can be represented in HCL.
type flagRule struct {
	Description types.String `tfsdk:"description"`
	Logic       types.String `tfsdk:"logic"`
	Value       types.String `tfsdk:"value"`
}

// flagValue mirrors a constrained value option (name/value pair).
type flagValue struct {
	Name  types.String `tfsdk:"name"`
	Value types.String `tfsdk:"value"`
}

type flagResourceModel struct {
	ID           types.String               `tfsdk:"id"`
	Name         types.String               `tfsdk:"name"`
	Type         types.String               `tfsdk:"type"`
	Description  types.String               `tfsdk:"description"`
	Default      types.String               `tfsdk:"default"`
	Values       []flagValue                `tfsdk:"values"`
	Environments map[string]flagEnvOverride `tfsdk:"environments"`
	CreatedAt    types.String               `tfsdk:"created_at"`
	UpdatedAt    types.String               `tfsdk:"updated_at"`
}

func (r *flagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flag"
}

func (r *flagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A smplkit feature flag. Carries a default value, an optional constrained value set, and " +
			"per-environment overrides (enable toggle, environment-specific default, ordered list of JSON Logic " +
			"targeting rules). `default`, rule `logic`, rule `value`, and constrained `value` entries are " +
			"JSON-encoded strings — use `jsonencode(...)` in HCL.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required: true,
				Description: "The flag key (e.g. `dark_mode`). Caller-supplied at create. " +
					"Use this same value as the `terraform import` identifier.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Display name shown in the smplkit console. Defaults to a humanized form of `id`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required: true,
				Description: "Flag value type: one of BOOLEAN, STRING, NUMERIC, or JSON. Selects the SDK constructor " +
					"and constrains the shape of `default`, `values`, and rule `value` fields.",
				Validators:    []validator.String{stringvalidator.OneOf(validFlagTypes...)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Optional free-text description. The server normalizes an absent description " +
					"to an empty string, so the attribute is also `Computed` — Terraform won't churn on the " +
					"null-vs-empty distinction.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"default": schema.StringAttribute{
				Required: true,
				Description: "JSON-encoded default value. Must match the flag `type`: BOOLEAN → `true`/`false`, " +
					"STRING → a JSON string (use `jsonencode(\"foo\")`), NUMERIC → a JSON number, JSON → any JSON " +
					"object. The provider canonicalizes serialization so plans don't churn on whitespace or key order.",
			},
			"values": schema.ListNestedAttribute{
				Optional: true,
				Description: "Optional closed set of allowed values for this flag. When omitted the flag accepts any " +
					"value of `type`; when set the console renders a dropdown of these named options. The Go SDK " +
					"client-side auto-populates `[{True,true},{False,false}]` for BOOLEAN flags; the provider " +
					"overrides that to nil when the user didn't supply a list, so the wire request omits values and " +
					"the state stays free of a server-default the user didn't ask for.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:    true,
							Description: "Human-readable label for this option.",
						},
						"value": schema.StringAttribute{
							Required:    true,
							Description: "JSON-encoded value to serve when this option is selected. Must match the flag `type`.",
						},
					},
				},
			},
			"environments": schema.MapNestedAttribute{
				Optional: true,
				Description: "Per-environment configuration keyed by environment id (e.g. `production`). Each entry " +
					"may set `enabled`, an environment-specific `default`, and an ordered list of `rules`.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"enabled": schema.BoolAttribute{
							Optional: true,
							Description: "Whether the flag is evaluated in this environment. " +
								"When false the flag returns the (env or base) default with no rule evaluation.",
						},
						"default": schema.StringAttribute{
							Optional: true,
							Description: "JSON-encoded environment-specific default. Falls back to the flag's base " +
								"`default` when omitted.",
						},
						"rules": schema.ListNestedAttribute{
							Optional: true,
							Description: "Ordered list of JSON Logic targeting rules. The first rule whose `logic` " +
								"evaluates truthy against the evaluation context serves its `value`.",
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"description": schema.StringAttribute{
										Required:    true,
										Description: "Human-readable description of what this rule targets.",
									},
									"logic": schema.StringAttribute{
										Required:    true,
										Description: "JSON Logic expression as a JSON-encoded string. See https://jsonlogic.com.",
									},
									"value": schema.StringAttribute{
										Required:    true,
										Description: "JSON-encoded value to serve when this rule matches.",
									},
								},
							},
						},
					},
				},
			},
			"created_at": schema.StringAttribute{
				Computed:      true,
				Description:   "RFC3339 timestamp set by the server when the flag was created.",
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

func (r *flagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// constructFlagFromModel builds an unsaved SDK Flag from the Terraform
// model. Centralised so Create can share the body with the SDK-side
// builders. The returned flag has its `default` set via the type-specific
// constructor so the SDK's `Type` and `Values` defaults take effect.
func (r *flagResource) constructFlagFromModel(data *flagResourceModel, diags *diag.Diagnostics) *smplkit.Flag {
	defaultVal, err := parseJSONString(data.Default.ValueString())
	if err != nil {
		diags.AddAttributeError(path.Root("default"), "Invalid JSON default", err.Error())
		return nil
	}

	opts := []smplkit.FlagOption{}
	if !data.Name.IsNull() && !data.Name.IsUnknown() && data.Name.ValueString() != "" {
		opts = append(opts, smplkit.WithFlagName(data.Name.ValueString()))
	}
	if !data.Description.IsNull() && !data.Description.IsUnknown() {
		opts = append(opts, smplkit.WithFlagDescription(data.Description.ValueString()))
	}
	if len(data.Values) > 0 {
		values, vdiags := flagValuesFromModel(data.Values)
		diags.Append(vdiags...)
		if vdiags.HasError() {
			return nil
		}
		opts = append(opts, smplkit.WithFlagValues(values))
	}

	var f *smplkit.Flag
	switch data.Type.ValueString() {
	case "BOOLEAN":
		b, ok := defaultVal.(bool)
		if !ok {
			diags.AddAttributeError(path.Root("default"), "Type mismatch", "BOOLEAN flag default must be `true` or `false`")
			return nil
		}
		f = r.client.Flags().NewBooleanFlag(data.ID.ValueString(), b, opts...)
	case "STRING":
		s, ok := defaultVal.(string)
		if !ok {
			diags.AddAttributeError(path.Root("default"), "Type mismatch", "STRING flag default must be a JSON-encoded string (e.g. jsonencode(\"foo\"))")
			return nil
		}
		f = r.client.Flags().NewStringFlag(data.ID.ValueString(), s, opts...)
	case "NUMERIC":
		n, ok := defaultVal.(float64)
		if !ok {
			diags.AddAttributeError(path.Root("default"), "Type mismatch", "NUMERIC flag default must be a JSON number")
			return nil
		}
		f = r.client.Flags().NewNumberFlag(data.ID.ValueString(), n, opts...)
	case "JSON":
		m, ok := defaultVal.(map[string]interface{})
		if !ok {
			diags.AddAttributeError(path.Root("default"), "Type mismatch", "JSON flag default must be a JSON object")
			return nil
		}
		f = r.client.Flags().NewJsonFlag(data.ID.ValueString(), m, opts...)
	default:
		diags.AddAttributeError(path.Root("type"), "Unknown flag type", fmt.Sprintf("type %q is not one of %v", data.Type.ValueString(), validFlagTypes))
		return nil
	}

	// NewBooleanFlag client-side seeds f.Values to a {True,False} option
	// set. When the user didn't actually supply a values block we want
	// the wire request to omit the field — otherwise the resource ends
	// up with a server-stored option set the user never asked for and
	// the next plan/apply churns to remove it. nil is the SDK signal
	// for "don't send."
	if len(data.Values) == 0 {
		f.Values = nil
	}

	// Populate per-environment overrides via the SDK's public mutators
	// rather than building the wire dict by hand — keeps us in lockstep
	// with whatever shape the SDK currently expects.
	for env, override := range data.Environments {
		if !override.Enabled.IsNull() && !override.Enabled.IsUnknown() {
			f.SetEnvironmentEnabled(env, override.Enabled.ValueBool())
		}
		if !override.Default.IsNull() && !override.Default.IsUnknown() && override.Default.ValueString() != "" {
			v, err := parseJSONString(override.Default.ValueString())
			if err != nil {
				diags.AddAttributeError(path.Root("environments").AtMapKey(env).AtName("default"), "Invalid JSON default", err.Error())
				return nil
			}
			f.SetEnvironmentDefault(env, v)
		}
		for i, rule := range override.Rules {
			logic, err := parseJSONString(rule.Logic.ValueString())
			if err != nil {
				diags.AddAttributeError(path.Root("environments").AtMapKey(env).AtName("rules").AtListIndex(i).AtName("logic"), "Invalid JSON Logic", err.Error())
				return nil
			}
			value, err := parseJSONString(rule.Value.ValueString())
			if err != nil {
				diags.AddAttributeError(path.Root("environments").AtMapKey(env).AtName("rules").AtListIndex(i).AtName("value"), "Invalid JSON value", err.Error())
				return nil
			}
			built := map[string]interface{}{
				"description": rule.Description.ValueString(),
				"logic":       logic,
				"value":       value,
				"environment": env,
			}
			if err := f.AddRule(built); err != nil {
				diags.AddAttributeError(path.Root("environments").AtMapKey(env).AtName("rules").AtListIndex(i), "Rule rejected", err.Error())
				return nil
			}
		}
	}

	return f
}

func (r *flagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data flagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	f := r.constructFlagFromModel(&data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := f.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("creating smplkit_flag %q", data.ID.ValueString()), err)
		return
	}

	applyFlagToModel(f, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *flagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data flagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	f, err := r.client.Flags().Get(ctx, data.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_flag %q", data.ID.ValueString()), err)
		return
	}

	applyFlagToModel(f, &data, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *flagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state flagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Construct a fresh flag carrying the plan's full shape, then stamp
	// CreatedAt on it so Save uses PUT semantics rather than POST.
	f := r.constructFlagFromModel(&plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// Surface a real timestamp; the value is only used to flip the
	// upsert switch in the SDK.
	existing, err := r.client.Flags().Get(ctx, state.ID.ValueString())
	if err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("reading smplkit_flag %q before update", state.ID.ValueString()), err)
		return
	}
	f.CreatedAt = existing.CreatedAt
	if err := f.Save(ctx); err != nil {
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("updating smplkit_flag %q", state.ID.ValueString()), err)
		return
	}

	applyFlagToModel(f, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *flagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data flagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Flags().Delete(ctx, data.ID.ValueString()); err != nil {
		if isNotFound(err) {
			return
		}
		addSDKErrorDiagnostic(&resp.Diagnostics, fmt.Sprintf("deleting smplkit_flag %q", data.ID.ValueString()), err)
	}
}

func (r *flagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func flagValuesFromModel(values []flagValue) ([]smplkit.FlagValue, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := make([]smplkit.FlagValue, 0, len(values))
	for i, v := range values {
		decoded, err := parseJSONString(v.Value.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("values").AtListIndex(i).AtName("value"), "Invalid JSON value", err.Error())
			continue
		}
		out = append(out, smplkit.FlagValue{Name: v.Name.ValueString(), Value: decoded})
	}
	return out, diags
}

// applyFlagToModel writes the server-returned flag back into the Terraform
// model, canonicalizing every JSON-encoded field so the next plan
// compares state and plan after the same normalization pass.
func applyFlagToModel(f *smplkit.Flag, model *flagResourceModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(f.ID)
	model.Name = types.StringValue(f.Name)
	model.Type = types.StringValue(f.Type)
	model.Description = stringPointerToTypes(f.Description)

	defStr, err := marshalCanonical(f.Default)
	if err != nil {
		diags.AddError("encoding flag default for state", err.Error())
		return
	}
	model.Default = types.StringValue(defStr)

	if f.Values == nil || len(*f.Values) == 0 {
		model.Values = nil
	} else {
		vs := make([]flagValue, 0, len(*f.Values))
		for _, v := range *f.Values {
			s, err := marshalCanonical(v.Value)
			if err != nil {
				diags.AddError("encoding flag value option for state", err.Error())
				return
			}
			vs = append(vs, flagValue{Name: types.StringValue(v.Name), Value: types.StringValue(s)})
		}
		model.Values = vs
	}

	if len(f.Environments) == 0 {
		model.Environments = nil
	} else {
		envs := make(map[string]flagEnvOverride, len(f.Environments))
		for env, raw := range f.Environments {
			inner, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			override := flagEnvOverride{}
			if e, ok := inner["enabled"].(bool); ok {
				override.Enabled = types.BoolValue(e)
			} else {
				override.Enabled = types.BoolNull()
			}
			if d, ok := inner["default"]; ok && d != nil {
				ds, err := marshalCanonical(d)
				if err != nil {
					diags.AddError("encoding per-environment flag default", err.Error())
					return
				}
				override.Default = types.StringValue(ds)
			} else {
				override.Default = types.StringNull()
			}
			rawRules, _ := inner["rules"].([]interface{})
			if len(rawRules) == 0 {
				override.Rules = nil
			} else {
				rules := make([]flagRule, 0, len(rawRules))
				for _, rr := range rawRules {
					rm, ok := rr.(map[string]interface{})
					if !ok {
						continue
					}
					descStr, _ := rm["description"].(string)
					logicStr, lerr := marshalCanonical(rm["logic"])
					if lerr != nil {
						diags.AddError("encoding rule logic", lerr.Error())
						return
					}
					valStr, verr := marshalCanonical(rm["value"])
					if verr != nil {
						diags.AddError("encoding rule value", verr.Error())
						return
					}
					rules = append(rules, flagRule{
						Description: types.StringValue(descStr),
						Logic:       types.StringValue(logicStr),
						Value:       types.StringValue(valStr),
					})
				}
				override.Rules = rules
			}
			envs[env] = override
		}
		if len(envs) == 0 {
			model.Environments = nil
		} else {
			model.Environments = envs
		}
	}

	model.CreatedAt = timePointerToString(f.CreatedAt)
	model.UpdatedAt = timePointerToString(f.UpdatedAt)
}

// flagEnvOverrideAttrTypes returns the underlying object-attribute type
// map for a flag environment override — needed when reconstructing the
// model in places where the framework wants explicit attr types.
//
// Kept here so the schema and the type wiring sit next to each other.
//
//nolint:unused // exported for documentation / future use; keep alongside the type.
func flagEnvOverrideAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"enabled": types.BoolType,
		"default": types.StringType,
		"rules": types.ListType{
			ElemType: types.ObjectType{
				AttrTypes: map[string]attr.Type{
					"description": types.StringType,
					"logic":       types.StringType,
					"value":       types.StringType,
				},
			},
		},
	}
}

// ensureValidJSON is a small helper that returns an error if the input is
// not parseable JSON; used by tests.
//
//nolint:unused // helper kept available for future schema validators.
func ensureValidJSON(s string) error {
	var v interface{}
	return json.Unmarshal([]byte(s), &v)
}
