package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

func fwdHeader(name, value string) forwarderHeader {
	return forwarderHeader{Name: types.StringValue(name), Value: types.StringValue(value)}
}

func TestAuditForwarderResource_Schema(t *testing.T) {
	r := NewAuditForwarderResource()
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	for _, name := range []string{"id", "name", "forwarder_type", "environments", "configuration"} {
		if _, ok := resp.Schema.Attributes[name]; !ok {
			t.Errorf("schema missing attribute %q", name)
		}
	}
}

func TestHTTPConfigurationFromModel_HeadersMap(t *testing.T) {
	model := &forwarderConfigurationModel{
		URL:           types.StringValue("https://siem.example.com/in"),
		Method:        types.StringValue("PUT"),
		SuccessStatus: types.StringValue("2xx"),
		Headers: []forwarderHeader{
			fwdHeader("DD-API-KEY", "secret"),
			fwdHeader("X-Source", "audit"),
		},
	}
	out := httpConfigurationFromModel(model)
	if out.URL != "https://siem.example.com/in" || out.Method != smplkit.HttpMethodPut || out.SuccessStatus != "2xx" {
		t.Errorf("scalars: %+v", out)
	}
	// Headers are an unordered name->value map since ADR-056.
	if len(out.Headers) != 2 || out.Headers["DD-API-KEY"] != "secret" || out.Headers["X-Source"] != "audit" {
		t.Errorf("headers not converted to map: %+v", out.Headers)
	}
}

func TestHTTPConfigurationFromModel_Nil(t *testing.T) {
	if out := httpConfigurationFromModel(nil); out.URL != "" || len(out.Headers) != 0 {
		t.Errorf("nil model should yield zero config: %+v", out)
	}
}

func TestConfigurationModelFromHTTP_HeadersSortedAndDefaults(t *testing.T) {
	cfg := configurationModelFromHTTP(smplkit.HttpConfiguration{
		URL:     "https://x.test",
		Headers: map[string]string{"B": "2", "A": "1"},
	})
	// Method / success_status default via the projection (not a schema default).
	if cfg.Method.ValueString() != "POST" || cfg.SuccessStatus.ValueString() != "2xx" {
		t.Errorf("documented defaults not applied: %+v", cfg)
	}
	// Map -> list is sorted by name for a deterministic baseline.
	if len(cfg.Headers) != 2 || cfg.Headers[0].Name.ValueString() != "A" || cfg.Headers[1].Name.ValueString() != "B" {
		t.Errorf("headers not sorted: %+v", cfg.Headers)
	}
}

func TestForwarderEnvironmentsFromModel_FlatLeaves(t *testing.T) {
	envs := forwarderEnvironmentsFromModel(map[string]forwarderEnvOverride{
		"production": {
			Enabled: types.BoolValue(true),
			Configuration: &forwarderConfigurationModel{
				URL:     types.StringValue("https://prod.siem/in"),
				Method:  types.StringValue("PUT"),
				Headers: []forwarderHeader{fwdHeader("DD-API-KEY", "prod")},
			},
		},
		// enabled omitted (null): a config-only override. Must project to the
		// flat URL leaf with Enabled defaulting to false — exercising the path
		// the per-env `enabled` Optional+Computed schema fix supports.
		"staging": {
			Enabled:       types.BoolNull(),
			Configuration: &forwarderConfigurationModel{URL: types.StringValue("https://stg.siem/in")},
		},
	})
	prod := envs["production"]
	if !prod.Enabled || prod.URL != "https://prod.siem/in" || prod.Method != smplkit.HttpMethodPut {
		t.Errorf("production flat leaves not converted: %+v", prod)
	}
	if prod.Headers["DD-API-KEY"] != "prod" {
		t.Errorf("production header override not converted: %+v", prod.Headers)
	}
	stg := envs["staging"]
	if stg.Enabled {
		t.Errorf("staging enabled should default to false when omitted: %+v", stg)
	}
	if stg.URL != "https://stg.siem/in" {
		t.Errorf("staging config-only override leaf not converted: %+v", stg)
	}
	if forwarderEnvironmentsFromModel(nil) != nil {
		t.Error("empty model map should convert to nil")
	}
}

func TestForwarderEnvConfigModel_Sparse(t *testing.T) {
	// Only URL and one header overridden — method/success_status must stay null
	// (sparse), NOT be defaulted to POST/2xx like the base configuration.
	cfg := forwarderEnvConfigModel(&smplkit.ForwarderEnvironment{
		URL:     "https://prod.siem/in",
		Headers: map[string]string{"X-Env": "prod"},
	})
	if cfg == nil {
		t.Fatal("a URL/header override should project a configuration block")
	}
	if cfg.URL.ValueString() != "https://prod.siem/in" {
		t.Errorf("url: %q", cfg.URL.ValueString())
	}
	if !cfg.Method.IsNull() || !cfg.SuccessStatus.IsNull() {
		t.Errorf("unset leaves must stay null (no base defaults), got method=%v status=%v", cfg.Method, cfg.SuccessStatus)
	}
	if len(cfg.Headers) != 1 || cfg.Headers[0].Name.ValueString() != "X-Env" {
		t.Errorf("headers: %+v", cfg.Headers)
	}
	// An environment overriding no configuration leaf projects no block.
	if forwarderEnvConfigModel(&smplkit.ForwarderEnvironment{Enabled: true}) != nil {
		t.Error("an enablement-only override should project a nil configuration")
	}
}

func TestApplyForwarderToModel_PerEnvSparseProjection(t *testing.T) {
	ver := 2
	fwd := &smplkit.Forwarder{
		ID:            "siem",
		Name:          "SIEM",
		ForwarderType: smplkit.ForwarderTypeDatadog,
		Configuration: smplkit.HttpConfiguration{URL: "https://base.siem/in"},
		Environments: map[string]*smplkit.ForwarderEnvironment{
			"production": {Enabled: true, URL: "https://prod.siem/in", Headers: map[string]string{"X-Env": "prod"}},
			"staging":    {Enabled: false},
		},
		Version: &ver,
	}
	var m auditForwarderResourceModel
	applyForwarderToModel(fwd, &m)
	if m.ID.ValueString() != "siem" || m.ForwarderType.ValueString() != "datadog" {
		t.Errorf("scalars: %+v", m)
	}
	prod, ok := m.Environments["production"]
	if !ok || !prod.Enabled.ValueBool() {
		t.Fatalf("production env not projected: %+v", m.Environments)
	}
	if prod.Configuration == nil || prod.Configuration.URL.ValueString() != "https://prod.siem/in" {
		t.Errorf("production sparse config override not projected: %+v", prod.Configuration)
	}
	// An enablement-only environment projects no configuration block.
	stg, ok := m.Environments["staging"]
	if !ok || stg.Enabled.ValueBool() {
		t.Errorf("staging should be present and disabled: %+v", stg)
	}
	if stg.Configuration != nil {
		t.Errorf("staging should have no configuration override: %+v", stg.Configuration)
	}
}
