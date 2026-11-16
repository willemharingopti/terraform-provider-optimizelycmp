package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &webhookResource{}
	_ resource.ResourceWithConfigure   = &webhookResource{}
	_ resource.ResourceWithImportState = &webhookResource{}
	_ resource.ResourceWithModifyPlan  = &webhookResource{}
)

var webhookKind = settingsKind{section: "webhooks", noun: "webhook"}

var httpsURL = regexp.MustCompile(`^https://[^\s/]+`)

func NewWebhookResource() resource.Resource { return &webhookResource{} }

type webhookResource struct{ c *client.Client }

type webhookModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	CallbackURL types.String `tfsdk:"callback_url"`
	Secret      types.String `tfsdk:"secret"`
	EventNames  types.Set    `tfsdk:"event_names"`
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A webhook, written through the API's bulk settings import (experimental). The import matches by name and never deletes, so: " +
			"event names can be added but not removed (the plan fails if you try), the name cannot change (a new name is a new webhook), " +
			"and destroying this resource only stops Terraform managing it. The API returns the secret in full, so it is stored in state.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The webhook name (the API gives webhooks no other identifier).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Webhook name. Changing it creates a new webhook and leaves the old one in CMP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Description."},
			"callback_url": schema.StringAttribute{Required: true, Description: "URL CMP calls. Must be https.",
				Validators: []validator.String{stringvalidator.RegexMatches(httpsURL, "must be an https URL")}},
			"secret": schema.StringAttribute{Required: true, Sensitive: true,
				Description: "Secret CMP uses to sign calls. It is stored in Terraform state; prefer a variable or secret store over writing it in configuration.",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)}},
			"event_names": schema.SetAttribute{Required: true, ElementType: types.StringType,
				Description: "Events to subscribe to. The settings import only accepted these when tested: asset_added, asset_modified, asset_removed, external_sub_step_comment_added/modified/removed, " +
					"external_sub_step_completed/modified/started, task_asset_added/modified/removed, task_asset_draft_added/removed, task_brief_added/modified/removed, task_custom_field_modified, " +
					"task_metadata_modified and task_removed. It rejects others that exist in CMP (such as task_added or work_request_added). Events can be added but not removed.",
				Validators: []validator.Set{setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1))}},
		},
	}
}

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *webhookModel) item(ctx context.Context) client.Webhook {
	return client.Webhook{
		Name: m.Name.ValueString(), Description: m.Description.ValueString(), CallbackURL: m.CallbackURL.ValueString(),
		Secret: m.Secret.ValueString(), EventNames: stringsOf(ctx, m.EventNames),
	}
}

func (r *webhookResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.EventNames.IsUnknown() {
		return
	}
	if gone := removedFrom(stringsOf(ctx, state.EventNames), stringsOf(ctx, plan.EventNames)); len(gone) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("event_names"), "Events cannot be removed",
			fmt.Sprintf("The CMP API can only add events to a webhook, never remove them, so removing %s would not take effect. "+
				"Keep the event in the configuration, or remove it in CMP and then here.", strings.Join(gone, ", ")))
	}
}

// refresh reads the webhook from the export. It reports whether it exists, and fails if the name is ambiguous.
func (r *webhookResource) refresh(ctx context.Context, m *webhookModel) (bool, error) {
	all, err := r.c.ExportWebhooks(ctx)
	if err != nil {
		return false, err
	}
	var hits []client.Webhook
	for _, w := range all {
		if w.Name == m.Name.ValueString() {
			hits = append(hits, w)
		}
	}
	if err := webhookKind.onlyOne(len(hits), m.Name.ValueString()); err != nil || len(hits) == 0 {
		return false, err
	}
	w := hits[0]
	m.ID, m.Name = types.StringValue(w.Name), types.StringValue(w.Name)
	m.Description, m.CallbackURL, m.Secret = types.StringValue(w.Description), types.StringValue(w.CallbackURL), types.StringValue(w.Secret)
	m.EventNames = stringSetOf(w.EventNames)
	return true, nil
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item := plan.item(ctx)
	if err := webhookKind.guardCreate(ctx, r.c, item.Name, item); err != nil {
		resp.Diagnostics.AddError("Creating webhook failed", err.Error())
		return
	}
	if err := webhookKind.write(ctx, r.c, item); err != nil {
		resp.Diagnostics.AddError("Creating webhook failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item := plan.item(ctx)
	if err := webhookKind.guardUpdate(ctx, r.c, item.Name, item); err != nil {
		resp.Diagnostics.AddError("Updating webhook failed", err.Error())
		return
	}
	if err := webhookKind.write(ctx, r.c, item); err != nil {
		resp.Diagnostics.AddError("Updating webhook failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

// finish reads the webhook back after a write and saves it. If the API did not apply what was asked, it says so
// (the state then shows what CMP really has).
func (r *webhookResource) finish(ctx context.Context, plan *webhookModel, sent client.Webhook, st *tfsdk.State, diags *diag.Diagnostics) {
	found, err := r.refresh(ctx, plan)
	if err != nil || !found {
		diags.AddError("Reading the webhook back failed", fmt.Sprintf("found=%v err=%v", found, err))
		return
	}
	diags.Append(st.Set(ctx, plan)...)
	got := plan.item(ctx)
	if got.CallbackURL != sent.CallbackURL || got.Secret != sent.Secret || got.Description != sent.Description || !sameStrings(got.EventNames, sent.EventNames) {
		diags.AddError("CMP did not apply the webhook as requested",
			"After writing, the webhook in CMP differs from the configuration. The state now shows what CMP has; run terraform plan to see the difference.")
	}
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.refresh(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Reading webhook failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete only forgets the webhook: the API cannot delete webhooks.
func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.AddWarning("Webhook not deleted",
		fmt.Sprintf("The CMP API cannot delete webhooks, so %q was only removed from Terraform state and still exists in CMP, still receiving events.", state.Name.ValueString()))
}

// ImportState takes the webhook's name.
func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
