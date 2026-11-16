package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var _ provider.Provider = &cmpProvider{}

type cmpProvider struct {
	version    string
	clientOpts []client.Option // only set by tests, to trust a mock server's certificate
}

// New returns the provider factory used by main.
func New(version string) func() provider.Provider { return newProvider(version) }

// newProvider is New with client options, which tests use to talk to a mock CMP over TLS.
func newProvider(version string, opts ...client.Option) func() provider.Provider {
	return func() provider.Provider { return &cmpProvider{version: version, clientOpts: opts} }
}

type providerModel struct {
	BaseURL      types.String `tfsdk:"base_url"`
	TokenURL     types.String `tfsdk:"token_url"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

func (p *cmpProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "optimizelycmp"
	resp.Version = p.version
}

func (p *cmpProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Optimizely CMP via its REST API.",
		Attributes: map[string]schema.Attribute{
			"base_url":      schema.StringAttribute{Optional: true, Description: "API host, https only; the /v3 version is added for you. Env: OPTIMIZELY_CMP_BASE_URL. Default " + client.DefaultHost},
			"token_url":     schema.StringAttribute{Optional: true, Description: "OAuth token URL (https only). Env: OPTIMIZELY_CMP_TOKEN_URL. Default " + client.DefaultTokenURL},
			"client_id":     schema.StringAttribute{Optional: true, Description: "API client ID. Env: OPTIMIZELY_CMP_CLIENT_ID."},
			"client_secret": schema.StringAttribute{Optional: true, Sensitive: true, Description: "API client secret. Prefer env OPTIMIZELY_CMP_CLIENT_SECRET so it stays out of config/state."},
		},
	}
}

func firstNonEmpty(v types.String, env, def string) string {
	if !v.IsNull() && !v.IsUnknown() && v.ValueString() != "" {
		return v.ValueString()
	}
	if e := os.Getenv(env); e != "" {
		return e
	}
	return def
}

func (p *cmpProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A value that is not known yet (for example one read from another resource) would be mistaken for "not set"
	// and replaced by an environment variable or a default, so refuse instead.
	for name, v := range map[string]types.String{"base_url": cfg.BaseURL, "token_url": cfg.TokenURL, "client_id": cfg.ClientID, "client_secret": cfg.ClientSecret} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(name), "Unknown provider setting",
				fmt.Sprintf("%s is not known until apply time, so the provider cannot be configured. Use a value that is known at plan time, such as a variable or an environment variable.", name))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := client.New(
		firstNonEmpty(cfg.BaseURL, "OPTIMIZELY_CMP_BASE_URL", client.DefaultHost),
		firstNonEmpty(cfg.TokenURL, "OPTIMIZELY_CMP_TOKEN_URL", ""),
		firstNonEmpty(cfg.ClientID, "OPTIMIZELY_CMP_CLIENT_ID", ""),
		firstNonEmpty(cfg.ClientSecret, "OPTIMIZELY_CMP_CLIENT_SECRET", ""),
		p.clientOpts...,
	)
	if err != nil {
		resp.Diagnostics.AddError("Invalid provider configuration", err.Error())
		return
	}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *cmpProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewFolderResource, NewFolderPermissionResource, NewContentTypeResource, NewFieldResource, NewLabelGroupResource, NewWebhookResource, NewAppResource, NewRoutingRuleResource}
}

func (p *cmpProvider) DataSources(context.Context) []func() datasource.DataSource {
	ds := []func() datasource.DataSource{NewUsersDataSource, NewSettingsDataSource}
	for _, spec := range lookupSpecs() {
		spec := spec
		ds = append(ds, func() datasource.DataSource { return &lookupDataSource{spec: spec} })
	}
	return ds
}

// clientFrom extracts the configured client during resource Configure.
func clientFrom(data any, diags *diag.Diagnostics) *client.Client {
	if data == nil {
		return nil // provider not configured yet (e.g. during validation)
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("got %T", data))
	}
	return c
}
