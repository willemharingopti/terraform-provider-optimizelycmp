package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &appResource{}
	_ resource.ResourceWithConfigure   = &appResource{}
	_ resource.ResourceWithImportState = &appResource{}
	_ resource.ResourceWithModifyPlan  = &appResource{}
)

var appKind = settingsKind{section: "apps", noun: "app"}

func NewAppResource() resource.Resource { return &appResource{} }

type appResource struct{ c *client.Client }

type appModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	HomepageURL  types.String `tfsdk:"homepage_url"`
	CallbackURLs types.Set    `tfsdk:"authorization_callback_urls"`
	ExposeEmail  types.Bool   `tfsdk:"expose_email"`
	Env          types.String `tfsdk:"env"`
}

func (r *appResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_app"
}

func (r *appResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	httpsOnly := []validator.String{stringvalidator.RegexMatches(httpsURL, "must be an https URL")}
	resp.Schema = schema.Schema{
		Description: "An app (an API client registration), written through the API's bulk settings import (experimental). The import matches by name " +
			"and never deletes, so: callback URLs can be added but not removed (the plan fails if you try), the name cannot change " +
			"(a new name is a new app), and destroying this resource only stops Terraform managing the app, which stays active in CMP.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The app name (the API gives apps no other identifier).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "App name. Changing it creates a new app and leaves the old one in CMP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description":  schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Description."},
			"homepage_url": schema.StringAttribute{Required: true, Description: "Home page URL. Must be https.", Validators: httpsOnly},
			"authorization_callback_urls": schema.SetAttribute{Required: true, ElementType: types.StringType,
				Description: "OAuth callback URLs (at least one, all https). URLs can be added but not removed.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.RegexMatches(httpsURL, "must be an https URL"))}},
			"expose_email": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Whether the API exposes users' email addresses to this app."},
			"env": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("production"),
				Description: "Environment. The API only accepted production when tested.",
				Validators:  []validator.String{stringvalidator.OneOf("production")}},
		},
	}
}

func (r *appResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *appModel) item(ctx context.Context) client.App {
	return client.App{
		Name: m.Name.ValueString(), Description: m.Description.ValueString(), HomepageURL: m.HomepageURL.ValueString(),
		AuthorizationCallbackURL: stringsOf(ctx, m.CallbackURLs), ExposeEmail: m.ExposeEmail.ValueBool(), Env: m.Env.ValueString(),
	}
}

func (r *appResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.CallbackURLs.IsUnknown() {
		return
	}
	if gone := removedFrom(stringsOf(ctx, state.CallbackURLs), stringsOf(ctx, plan.CallbackURLs)); len(gone) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("authorization_callback_urls"), "Callback URLs cannot be removed",
			fmt.Sprintf("The CMP API can only add callback URLs to an app, never remove them, so removing %s would not take effect. "+
				"Keep the URL in the configuration, or remove it in CMP and then here.", strings.Join(gone, ", ")))
	}
}

func (r *appResource) refresh(ctx context.Context, m *appModel) (bool, error) {
	all, err := r.c.ExportApps(ctx)
	if err != nil {
		return false, err
	}
	var hits []client.App
	for _, a := range all {
		if a.Name == m.Name.ValueString() {
			hits = append(hits, a)
		}
	}
	if err := appKind.onlyOne(len(hits), m.Name.ValueString()); err != nil || len(hits) == 0 {
		return false, err
	}
	a := hits[0]
	m.ID, m.Name = types.StringValue(a.Name), types.StringValue(a.Name)
	m.Description, m.HomepageURL = types.StringValue(a.Description), types.StringValue(a.HomepageURL)
	m.CallbackURLs = stringSetOf(a.AuthorizationCallbackURL)
	m.ExposeEmail, m.Env = types.BoolValue(a.ExposeEmail), types.StringValue(a.Env)
	return true, nil
}

func (r *appResource) finish(ctx context.Context, plan *appModel, sent client.App, st *tfsdk.State, diags *diag.Diagnostics) {
	found, err := r.refresh(ctx, plan)
	if err != nil || !found {
		diags.AddError("Reading the app back failed", fmt.Sprintf("found=%v err=%v", found, err))
		return
	}
	diags.Append(st.Set(ctx, plan)...)
	got := plan.item(ctx)
	if got.HomepageURL != sent.HomepageURL || got.Description != sent.Description || got.ExposeEmail != sent.ExposeEmail || got.Env != sent.Env ||
		!sameStrings(got.AuthorizationCallbackURL, sent.AuthorizationCallbackURL) {
		diags.AddError("CMP did not apply the app as requested",
			"After writing, the app in CMP differs from the configuration. The state now shows what CMP has; run terraform plan to see the difference.")
	}
}

func (r *appResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item := plan.item(ctx)
	if err := appKind.guardCreate(ctx, r.c, item.Name, item); err != nil {
		resp.Diagnostics.AddError("Creating app failed", err.Error())
		return
	}
	if err := appKind.write(ctx, r.c, item); err != nil {
		resp.Diagnostics.AddError("Creating app failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

func (r *appResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan appModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item := plan.item(ctx)
	if err := appKind.guardUpdate(ctx, r.c, item.Name, item); err != nil {
		resp.Diagnostics.AddError("Updating app failed", err.Error())
		return
	}
	if err := appKind.write(ctx, r.c, item); err != nil {
		resp.Diagnostics.AddError("Updating app failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

func (r *appResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.refresh(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Reading app failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete only forgets the app: the API cannot delete apps.
func (r *appResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state appModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.AddWarning("App not deleted",
		fmt.Sprintf("The CMP API cannot delete apps, so %q was only removed from Terraform state and still exists in CMP.", state.Name.ValueString()))
}

// ImportState takes the app's name.
func (r *appResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
