package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &contentTypeResource{}
	_ resource.ResourceWithConfigure   = &contentTypeResource{}
	_ resource.ResourceWithImportState = &contentTypeResource{}
	_ resource.ResourceWithModifyPlan  = &contentTypeResource{}
)

func NewContentTypeResource() resource.Resource { return &contentTypeResource{} }

type contentTypeResource struct{ c *client.Client }

type contentTypeModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Description      types.String `tfsdk:"description"`
	Component        types.Bool   `tfsdk:"component"`
	Disabled         types.Bool   `tfsdk:"disabled"`
	ThumbnailGUID    types.String `tfsdk:"thumbnail_guid"`
	ActingUserID     types.String `tfsdk:"acting_user_id"`
	FieldDefinitions types.String `tfsdk:"field_definitions_json"`
	ExpectedLocales  types.Set    `tfsdk:"expected_locales"`
	LatestVersionID  types.String `tfsdk:"latest_version_id"`
	CreatedAt        types.String `tfsdk:"created_at"`
	UpdatedAt        types.String `tfsdk:"updated_at"`
}

func (r *contentTypeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_content_type"
}

func (r *contentTypeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A structured content type. The API has no delete: destroying this resource **disables** the content type " +
			"and removes it from Terraform state. The API marks these endpoints as experimental.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Content type GUID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":        schema.StringAttribute{Required: true, Description: "Name of the content type."},
			"description": schema.StringAttribute{Optional: true, Description: "Description."},
			"component":   schema.BoolAttribute{Required: true, Description: "Whether the type is a component (embedded in other content) rather than standalone content."},
			"disabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Whether the content type is disabled."},
			"thumbnail_guid": schema.StringAttribute{Optional: true, Description: "Thumbnail GUID."},
			"acting_user_id": schema.StringAttribute{Required: true,
				Description: "ID of the CMP user recorded as creator/updater (the API requires one). Use the optimizelycmp_user data source. Changing it alone makes no API call."},
			"field_definitions_json": schema.StringAttribute{Required: true,
				Description: "JSON array of field definitions, exactly as the API defines them (use jsonencode). Changing it publishes a new content type version. Server-added defaults are ignored when checking for drift."},
			"expected_locales": schema.SetAttribute{Optional: true, ElementType: types.StringType,
				Description: "Locales the content is expected in. Changing it publishes a new version."},
			"latest_version_id": schema.StringAttribute{Computed: true, Description: "GUID of the latest version."},
			"created_at":        schema.StringAttribute{Computed: true, Description: "Creation time (ISO 8601 UTC)."},
			"updated_at":        schema.StringAttribute{Computed: true, Description: "Last update time (ISO 8601 UTC)."},
		},
	}
}

func (r *contentTypeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (m *contentTypeModel) details() client.ContentTypeDetails {
	return client.ContentTypeDetails{
		Name:          m.Name.ValueString(),
		Description:   optString(m.Description),
		Component:     m.Component.ValueBool(),
		Disabled:      m.Disabled.ValueBool(),
		ThumbnailGUID: optString(m.ThumbnailGUID),
	}
}

func localesOf(ctx context.Context, s types.Set) ([]string, error) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	var out []string
	if d := s.ElementsAs(ctx, &out, false); d.HasError() {
		return nil, fmt.Errorf("reading expected_locales: %v", d)
	}
	return out, nil
}

func validateFieldDefs(s string) error {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return fmt.Errorf("field_definitions_json must be a JSON array: %w", err)
	}
	return nil
}

// refresh copies API values into m. The user's field_definitions_json is kept when the API's version
// still covers it, so server-added defaults do not show up as drift.
func (r *contentTypeResource) refresh(ctx context.Context, ct *client.ContentType, m *contentTypeModel) error {
	m.ID = types.StringValue(ct.GUID)
	m.Name = types.StringValue(ct.Name)
	m.Component = types.BoolValue(ct.Component)
	m.Disabled = types.BoolValue(ct.Disabled)
	m.CreatedAt = types.StringValue(ct.CreatedAt)
	m.UpdatedAt = types.StringValue(ct.UpdatedAt)
	m.Description = nullableString(ct.Description)
	m.ThumbnailGUID = nullableString(ct.ThumbnailGUID)
	prevVersion := m.LatestVersionID
	m.LatestVersionID = types.StringNull()
	if v := ct.LatestVersion; v != nil {
		m.LatestVersionID = types.StringValue(v.VersionGUID)
		// The API normalises field definitions (for example an empty validation_pattern comes back as ".*"),
		// so comparing JSON would show permanent drift. A new version means someone changed the fields outside
		// Terraform; the same version means they are as Terraform last applied them.
		sameVersion := !prevVersion.IsNull() && !prevVersion.IsUnknown() && prevVersion.ValueString() == v.VersionGUID
		keep := !m.FieldDefinitions.IsNull() && !m.FieldDefinitions.IsUnknown() &&
			(sameVersion || jsonCovers(m.FieldDefinitions.ValueString(), v.FieldDefs))
		if !keep {
			m.FieldDefinitions = types.StringValue(compactJSON(v.FieldDefs))
		}
		if sameVersion && !m.ExpectedLocales.IsNull() {
			return nil
		}
		if len(v.ExpectedLocales) > 0 || !m.ExpectedLocales.IsNull() {
			elems := make([]attr.Value, len(v.ExpectedLocales))
			for i, l := range v.ExpectedLocales {
				elems[i] = types.StringValue(l)
			}
			set, d := types.SetValue(types.StringType, elems)
			if d.HasError() {
				return fmt.Errorf("expected_locales: %v", d)
			}
			m.ExpectedLocales = set
		}
	}
	return nil
}

func nullableString(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func (r *contentTypeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan contentTypeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateFieldDefs(plan.FieldDefinitions.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("field_definitions_json"), "Invalid field definitions", err.Error())
		return
	}
	locales, err := localesOf(ctx, plan.ExpectedLocales)
	if err != nil {
		resp.Diagnostics.AddError("Invalid expected_locales", err.Error())
		return
	}
	id, err := r.c.CreateContentType(ctx, plan.details(), json.RawMessage(plan.FieldDefinitions.ValueString()), locales, plan.ActingUserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Creating content type failed", err.Error())
		return
	}
	// Save the ID first so a failed read-back does not orphan the content type.
	plan.ID = types.StringValue(id)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	ct, err := r.c.GetContentType(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading the new content type failed", err.Error())
		return
	}
	fd, loc := plan.FieldDefinitions, plan.ExpectedLocales
	if err := r.refresh(ctx, ct, &plan); err != nil {
		resp.Diagnostics.AddError("Reading the new content type failed", err.Error())
		return
	}
	plan.FieldDefinitions, plan.ExpectedLocales = fd, loc // what was applied; real drift shows on the next refresh
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *contentTypeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state contentTypeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ct, err := r.c.GetContentType(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading content type failed", err.Error())
		return
	}
	if err := r.refresh(ctx, ct, &state); err != nil {
		resp.Diagnostics.AddError("Reading content type failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *contentTypeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state contentTypeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, by := state.ID.ValueString(), plan.ActingUserID.ValueString()

	if !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) || !plan.Component.Equal(state.Component) ||
		!plan.Disabled.Equal(state.Disabled) || !plan.ThumbnailGUID.Equal(state.ThumbnailGUID) {
		d := plan.details()
		if plan.Description.IsNull() && !state.Description.IsNull() {
			empty := ""
			d.Description = &empty // clear it
		}
		if plan.ThumbnailGUID.IsNull() && !state.ThumbnailGUID.IsNull() {
			empty := ""
			d.ThumbnailGUID = &empty
		}
		if err := r.c.UpdateContentType(ctx, id, d, by); err != nil {
			resp.Diagnostics.AddError("Updating content type failed", err.Error())
			return
		}
	}

	if !plan.FieldDefinitions.Equal(state.FieldDefinitions) || !plan.ExpectedLocales.Equal(state.ExpectedLocales) {
		if err := validateFieldDefs(plan.FieldDefinitions.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("field_definitions_json"), "Invalid field definitions", err.Error())
			return
		}
		locales, err := localesOf(ctx, plan.ExpectedLocales)
		if err != nil {
			resp.Diagnostics.AddError("Invalid expected_locales", err.Error())
			return
		}
		if err := r.c.AddContentTypeVersion(ctx, id, json.RawMessage(plan.FieldDefinitions.ValueString()), locales, by); err != nil {
			resp.Diagnostics.AddError("Publishing a new content type version failed", err.Error())
			return
		}
	}

	ct, err := r.c.GetContentType(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading content type failed", err.Error())
		return
	}
	plan.ID = state.ID
	fd, loc := plan.FieldDefinitions, plan.ExpectedLocales
	if err := r.refresh(ctx, ct, &plan); err != nil {
		resp.Diagnostics.AddError("Reading content type failed", err.Error())
		return
	}
	plan.FieldDefinitions, plan.ExpectedLocales = fd, loc
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete disables the content type: the API has no delete endpoint.
func (r *contentTypeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state contentTypeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d := state.details()
	d.Disabled = true
	err := r.c.UpdateContentType(ctx, state.ID.ValueString(), d, state.ActingUserID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Disabling content type failed", err.Error())
		return
	}
	resp.Diagnostics.AddWarning("Content type disabled, not deleted",
		fmt.Sprintf("The CMP API has no delete for content types, so %q was disabled and removed from state.", state.Name.ValueString()))
}

func (r *contentTypeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ModifyPlan keeps computed values stable when nothing changes, and marks only what will really change as unknown:
// the update time on any change, and the version ID only when a new version will be published.
func (r *contentTypeResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create or destroy
	}
	var plan, state contentTypeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	newVersion := !plan.FieldDefinitions.Equal(state.FieldDefinitions) || !plan.ExpectedLocales.Equal(state.ExpectedLocales)
	detailsChanged := !plan.Name.Equal(state.Name) || !plan.Description.Equal(state.Description) || !plan.Component.Equal(state.Component) ||
		!plan.Disabled.Equal(state.Disabled) || !plan.ThumbnailGUID.Equal(state.ThumbnailGUID)
	plan.CreatedAt = state.CreatedAt
	if newVersion {
		plan.LatestVersionID = types.StringUnknown()
	} else {
		plan.LatestVersionID = state.LatestVersionID
	}
	if newVersion || detailsChanged {
		plan.UpdatedAt = types.StringUnknown()
	} else {
		plan.UpdatedAt = state.UpdatedAt
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}
