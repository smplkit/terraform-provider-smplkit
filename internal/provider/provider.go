// Package provider implements the Terraform provider for smplkit.
package provider

import (
	"context"
	"net/url"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	smplkit "github.com/smplkit/go-sdk/v3"
)

const envAPIKey = "SMPLKIT_API_KEY"

// SmplkitProvider wires the smplkit management client into Terraform.
type SmplkitProvider struct {
	version string
}

// New returns a Terraform provider factory bound to the build's version
// string. The version is stamped into the User-Agent so the platform can
// distinguish Terraform traffic.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &SmplkitProvider{version: version}
	}
}

// SmplkitProviderModel holds the parsed provider configuration.
type SmplkitProviderModel struct {
	APIKey types.String `tfsdk:"api_key"`
	APIURL types.String `tfsdk:"api_url"`
}

// Metadata sets the provider's local name and version.
func (p *SmplkitProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "smplkit"
	resp.Version = p.version
}

// Schema describes the provider configuration block.
func (p *SmplkitProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages smplkit platform resources (configurations, flags, audit forwarders, log groups, environments, services, scheduled jobs) " +
			"through the smplkit management API. The provider takes a normal dependency on the public Go SDK and calls only " +
			"the management client — no hand-rolled HTTP, no auto-registration side effects.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "API key for the smplkit platform. May be set via the SMPLKIT_API_KEY environment variable. Create one in the smplkit console.",
			},
			"api_url": schema.StringAttribute{
				Optional: true,
				Description: "Override the default API base URL. Useful for running against the local platform " +
					"(`http://localhost:8000`, etc.). When unset, the provider hits the production smplkit.com endpoints. " +
					"Accepts the app host; the management client reaches sibling product services through subdomain conventions.",
			},
		},
	}
}

// Configure builds a management client from the resolved api_key / api_url
// and stashes it on the response so resources and data sources can read it.
func (p *SmplkitProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data SmplkitProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := strings.TrimSpace(data.APIKey.ValueString())
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv(envAPIKey))
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing smplkit API key",
			"Set the provider's `api_key` attribute or the SMPLKIT_API_KEY environment variable. "+
				"Create an API key in the smplkit console (Settings → API keys).",
		)
		return
	}

	cfg := smplkit.Config{APIKey: apiKey}
	if !data.APIURL.IsNull() && !data.APIURL.IsUnknown() {
		raw := strings.TrimSpace(data.APIURL.ValueString())
		if raw != "" {
			scheme, base, err := splitAPIURL(raw)
			if err != nil {
				resp.Diagnostics.AddAttributeError(
					path.Root("api_url"),
					"Invalid api_url",
					err.Error(),
				)
				return
			}
			cfg.Scheme = scheme
			cfg.BaseDomain = base
		}
	}

	client, err := smplkit.NewClient(cfg)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to construct smplkit management client",
			"The SDK rejected the supplied configuration. "+err.Error(),
		)
		return
	}

	resp.ResourceData = client
	resp.DataSourceData = client
}

// Resources lists the resource types this provider exposes.
func (p *SmplkitProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewFlagResource,
		NewConfigurationResource,
		NewAuditForwarderResource,
		NewLogGroupResource,
		NewEnvironmentResource,
		NewServiceResource,
		NewJobResource,
	}
}

// DataSources lists the read-only data sources this provider exposes.
func (p *SmplkitProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewFlagDataSource,
		NewConfigurationDataSource,
		NewAuditForwarderDataSource,
		NewLogGroupDataSource,
		NewEnvironmentDataSource,
		NewServiceDataSource,
		NewJobDataSource,
	}
}

// splitAPIURL accepts a URL like "http://localhost:8000" or "app.smplkit.com"
// and returns the scheme and "base domain" the SDK expects (the SDK builds
// per-service URLs by prepending `<service>.` to BaseDomain).
//
// To target the local platform the user sets api_url to
// `http://localhost:8000`; we extract scheme "http" and base "localhost:8000".
// The management client derives flags/config/logging/audit base URLs by
// substituting subdomains, so the host *must* be reachable for all four
// services through subdomain conventions in production. For the local
// platform, the `*.localhost:NNNN` convention takes over (ADR-042).
func splitAPIURL(raw string) (scheme, base string, err error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, perr := url.Parse(raw)
	if perr != nil {
		return "", "", perr
	}
	if parsed.Host == "" {
		return "", "", &urlError{msg: "api_url must include a host"}
	}
	scheme = parsed.Scheme
	if scheme == "" {
		scheme = "https"
	}
	base = parsed.Host
	// Strip a leading `app.` so the SDK's serviceURL helper rebuilds it.
	base = strings.TrimPrefix(base, "app.")
	return scheme, base, nil
}

type urlError struct{ msg string }

func (e *urlError) Error() string { return e.msg }
