package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ datasource.DataSource              = &settingsDataSource{}
	_ datasource.DataSourceWithConfigure = &settingsDataSource{}
)

func NewSettingsDataSource() datasource.DataSource { return &settingsDataSource{} }

type settingsDataSource struct{ c *client.Client }

type settingsModel struct {
	ID               types.String `tfsdk:"id"`
	LabelsJSON       types.String `tfsdk:"labels_json"`
	CustomFieldsJSON types.String `tfsdk:"custom_fields_json"`
	WorkflowsJSON    types.String `tfsdk:"workflows_json"`
	TemplatesJSON    types.String `tfsdk:"templates_json"`
	AppsJSON         types.String `tfsdk:"apps_json"`
	WebhooksJSON     types.String `tfsdk:"webhooks_json"`
	RoutingRulesJSON types.String `tfsdk:"routing_rules_json"`
}

func (d *settingsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_settings"
}

func (d *settingsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	section := func(desc string) schema.Attribute {
		return schema.StringAttribute{Computed: true, Description: desc + " As a JSON string in the API's own shape; read it with jsondecode()."}
	}
	resp.Schema = schema.Schema{
		Description: "A read-only export of the organisation's configuration (the API's GET /settings; experimental). " +
			"Each section is returned as JSON text so it keeps the API's shape. Nothing is changed in CMP.",
		Attributes: map[string]schema.Attribute{
			"id":                 schema.StringAttribute{Computed: true, Description: "Always \"settings\"; present because every data source needs an ID."},
			"labels_json":        section("Label groups and their labels."),
			"custom_fields_json": section("Custom fields: an object keyed by field type (currency, dropdown, ...), each holding a list."),
			"workflows_json":     section("Workflows, with their steps, labels, custom fields and channels."),
			"templates_json":     section("Templates, with their form fields, sections and instructions."),
			"apps_json":          section("Apps."),
			"webhooks_json": schema.StringAttribute{Computed: true, Sensitive: true,
				Description: "Webhooks. **Sensitive**: the export includes each webhook's secret. As a JSON string in the API's own shape; read it with jsondecode()."},
			"routing_rules_json": section("Routing rules."),
		},
	}
}

func (d *settingsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *settingsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	res, err := d.c.GetSettings(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Reading settings failed", err.Error())
		return
	}
	get := func(name, empty string) types.String {
		if raw, ok := res[name]; ok && len(raw) > 0 && string(raw) != "null" {
			return types.StringValue(compactJSON(raw))
		}
		return types.StringValue(empty)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, settingsModel{
		ID:               types.StringValue("settings"),
		LabelsJSON:       get("labels", "[]"),
		CustomFieldsJSON: get("custom_fields", "{}"),
		WorkflowsJSON:    get("workflows", "[]"),
		TemplatesJSON:    get("templates", "[]"),
		AppsJSON:         get("apps", "[]"),
		WebhooksJSON:     get("webhooks", "[]"),
		RoutingRulesJSON: get("routing_rules", "[]"),
	})...)
}
