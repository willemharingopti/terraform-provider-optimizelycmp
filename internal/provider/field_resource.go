package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &fieldResource{}
	_ resource.ResourceWithConfigure   = &fieldResource{}
	_ resource.ResourceWithImportState = &fieldResource{}
	_ resource.ResourceWithModifyPlan  = &fieldResource{}
)

// fieldTypes are the custom field types the API accepts.
var fieldTypes = []string{"text", "text_area", "rich_text", "date", "image", "video", "simple_number", "percentage_number",
	"currency_number", "dropdown", "label", "checkbox", "radio_button"}

func hasChoices(t string) bool {
	return t == "dropdown" || t == "label" || t == "checkbox" || t == "radio_button"
}
func hasMultiSelect(t string) bool { return t == "dropdown" || t == "label" }

func NewFieldResource() resource.Resource { return &fieldResource{} }

type fieldResource struct{ c *client.Client }

type fieldModel struct {
	ID                   types.String `tfsdk:"id"`
	Name                 types.String `tfsdk:"name"`
	Type                 types.String `tfsdk:"type"`
	HelperText           types.String `tfsdk:"helper_text"`
	IsActive             types.Bool   `tfsdk:"is_active"`
	IsMultiSelect        types.Bool   `tfsdk:"is_multi_select"`
	HasThousandSeparator types.Bool   `tfsdk:"has_thousand_separator"`
	DecimalPlaces        types.Int64  `tfsdk:"decimal_places"`
	CurrencyCode         types.String `tfsdk:"currency_code"`
	Choices              types.Map    `tfsdk:"choices"`
}

type choiceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Color    types.String `tfsdk:"color"`
	IsActive types.Bool   `tfsdk:"is_active"`
}

var choiceAttrTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "color": types.StringType, "is_active": types.BoolType,
}
var choiceObjType = types.ObjectType{AttrTypes: choiceAttrTypes}

func (r *fieldResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_field"
}

func (r *fieldResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "An organisation-level custom field (used on tasks, campaigns and workflows). The API has no delete: " +
			"destroying this resource **deactivates** the field and removes it from state. The API marks these endpoints as experimental.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, Description: "Field ID.", PlanModifiers: keep},
			"name": schema.StringAttribute{Required: true, Description: "Field name."},
			"type": schema.StringAttribute{Required: true, Description: "Field type. It cannot be changed: changing it replaces the field (the old one is left behind, deactivated).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{stringvalidator.OneOf(fieldTypes...)}},
			"helper_text": schema.StringAttribute{Optional: true, Description: "Text that explains the field to users."},
			"is_active": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Inactive fields cannot be used in tasks, campaigns or workflows."},
			"is_multi_select": schema.BoolAttribute{Optional: true, Computed: true, Description: "Allow several values (dropdown and label types only; defaults to false there).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
			"has_thousand_separator": schema.BoolAttribute{Optional: true, Computed: true, Description: "Number types only.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
			"decimal_places": schema.Int64Attribute{Optional: true, Computed: true, Description: "Number types only. Must be greater than 0; the API defaults it to 2.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"currency_code": schema.StringAttribute{Optional: true, Computed: true, Description: "currency_number type only. A valid currency code; the API defaults it to USD.",
				PlanModifiers: keep},
			"choices": schema.MapNestedAttribute{
				Optional: true,
				Description: "Choices of a dropdown, label, checkbox or radio_button field, keyed by a name of your choosing that Terraform uses to track each one " +
					"(so renaming a choice does not recreate it). Choices are required for those types and not allowed for others. " +
					"Choices added in CMP outside Terraform are ignored. The API refuses to delete the last active choice.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":    schema.StringAttribute{Computed: true, Description: "Choice ID.", PlanModifiers: keep},
					"name":  schema.StringAttribute{Required: true, Description: "Choice name; must be unique within the field."},
					"color": schema.StringAttribute{Optional: true, Description: "label type only: one of #4ECFD5, #FFC700, #FF98A7, #702BD5, #5FEEAD, #D6C4F2, #5F9FF6, #CB5DEB, #9694B3."},
					"is_active": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true),
						Description: "Inactive choices stay on the field but users cannot pick them."},
				}},
			},
		},
	}
}

func (r *fieldResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func choicesOf(ctx context.Context, m types.Map) (map[string]choiceModel, error) {
	out := map[string]choiceModel{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	if d := m.ElementsAs(ctx, &out, false); d.HasError() {
		return nil, fmt.Errorf("reading choices: %v", d)
	}
	return out, nil
}

func choicesToMap(ctx context.Context, in map[string]choiceModel) (types.Map, error) {
	v, d := types.MapValueFrom(ctx, choiceObjType, in)
	if d.HasError() {
		return types.MapNull(choiceObjType), fmt.Errorf("building choices: %v", d)
	}
	return v, nil
}

func sortedKeys(m map[string]choiceModel) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func validateField(plan fieldModel, choices map[string]choiceModel) error {
	t := plan.Type.ValueString()
	switch {
	case hasChoices(t) && len(choices) == 0:
		return fmt.Errorf("a %s field needs at least one choice", t)
	case !hasChoices(t) && len(choices) > 0:
		return fmt.Errorf("a %s field cannot have choices", t)
	}
	seen := map[string]bool{}
	for _, k := range sortedKeys(choices) {
		n := choices[k].Name.ValueString()
		if seen[n] {
			return fmt.Errorf("choice name %q is used twice; names must be unique within a field", n)
		}
		seen[n] = true
	}
	return nil
}

func choicePayload(c choiceModel) map[string]any {
	p := map[string]any{"name": c.Name.ValueString()}
	if !c.Color.IsNull() && !c.Color.IsUnknown() {
		p["color"] = c.Color.ValueString()
	}
	return p
}

// refresh copies API values into m. Choices are matched to their Terraform keys by ID; choices the API has but
// Terraform does not track are ignored, and tracked choices the API no longer has are dropped.
func (r *fieldResource) refresh(ctx context.Context, f *client.Field, m *fieldModel) error {
	m.ID = types.StringValue(f.ID)
	m.Name = types.StringValue(f.Name)
	m.Type = types.StringValue(f.Type)
	m.IsActive = types.BoolValue(f.IsActive)
	m.HelperText = nullableString(f.HelperText)
	m.IsMultiSelect, m.HasThousandSeparator = types.BoolNull(), types.BoolNull()
	m.DecimalPlaces, m.CurrencyCode = types.Int64Null(), types.StringNull()
	if f.IsMultiSelect != nil {
		m.IsMultiSelect = types.BoolValue(*f.IsMultiSelect)
	}
	if f.HasThousandSeparator != nil {
		m.HasThousandSeparator = types.BoolValue(*f.HasThousandSeparator)
	}
	if f.DecimalPlaces != nil {
		m.DecimalPlaces = types.Int64Value(int64(*f.DecimalPlaces))
	}
	if f.CurrencyCode != nil && *f.CurrencyCode != "" {
		m.CurrencyCode = types.StringValue(*f.CurrencyCode)
	}
	if !hasChoices(f.Type) {
		m.Choices = types.MapNull(choiceObjType)
		return nil
	}
	tracked, err := choicesOf(ctx, m.Choices)
	if err != nil {
		return err
	}
	byID := map[string]client.FieldChoice{}
	for _, c := range f.Choices {
		byID[c.ID] = c
	}
	out := map[string]choiceModel{}
	for k, old := range tracked {
		c, ok := byID[old.ID.ValueString()]
		if !ok {
			continue
		}
		out[k] = choiceModel{ID: types.StringValue(c.ID), Name: types.StringValue(c.Name), Color: nullableString(c.Color), IsActive: types.BoolValue(c.IsActive)}
	}
	m.Choices, err = choicesToMap(ctx, out)
	return err
}

// assignChoiceIDs fills in the IDs of choices (matched by name) after they were created.
func assignChoiceIDs(f *client.Field, want map[string]choiceModel) error {
	byName := map[string]client.FieldChoice{}
	for _, c := range f.Choices {
		byName[c.Name] = c
	}
	for k, c := range want {
		if !c.ID.IsNull() && !c.ID.IsUnknown() {
			continue
		}
		got, ok := byName[c.Name.ValueString()]
		if !ok {
			return fmt.Errorf("choice %q was not found on the field after creating it", c.Name.ValueString())
		}
		c.ID = types.StringValue(got.ID)
		want[k] = c
	}
	return nil
}

func (r *fieldResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	choices, err := choicesOf(ctx, plan.Choices)
	if err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	if err := validateField(plan, choices); err != nil {
		resp.Diagnostics.AddError("Invalid field", err.Error())
		return
	}
	t := plan.Type.ValueString()
	payload := map[string]any{"name": plan.Name.ValueString(), "type": t, "is_active": plan.IsActive.ValueBool()}
	if v := optString(plan.HelperText); v != nil {
		payload["helper_text"] = *v
	}
	if hasMultiSelect(t) {
		payload["is_multi_select"] = !plan.IsMultiSelect.IsNull() && !plan.IsMultiSelect.IsUnknown() && plan.IsMultiSelect.ValueBool()
	}
	if !plan.HasThousandSeparator.IsNull() && !plan.HasThousandSeparator.IsUnknown() {
		payload["has_thousand_separator"] = plan.HasThousandSeparator.ValueBool()
	}
	if !plan.DecimalPlaces.IsNull() && !plan.DecimalPlaces.IsUnknown() {
		payload["decimal_places"] = plan.DecimalPlaces.ValueInt64()
	}
	if v := optString(plan.CurrencyCode); v != nil {
		payload["currency_code"] = *v
	}
	if hasChoices(t) {
		list := make([]map[string]any, 0, len(choices))
		for _, k := range sortedKeys(choices) {
			list = append(list, choicePayload(choices[k]))
		}
		payload["choices"] = list
	}
	id, err := r.c.CreateField(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Creating field failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...) // so a failed read-back does not orphan it
	f, err := r.c.GetField(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading the new field failed", err.Error())
		return
	}
	if hasChoices(t) {
		if err := assignChoiceIDs(f, choices); err != nil {
			resp.Diagnostics.AddError("Reading the new field's choices failed", err.Error())
			return
		}
		if plan.Choices, err = choicesToMap(ctx, choices); err != nil {
			resp.Diagnostics.AddError("Reading the new field's choices failed", err.Error())
			return
		}
	}
	plan.ID = types.StringValue(id)
	if err := r.refresh(ctx, f, &plan); err != nil {
		resp.Diagnostics.AddError("Reading the new field failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *fieldResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.GetField(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading field failed", err.Error())
		return
	}
	if err := r.refresh(ctx, f, &state); err != nil {
		resp.Diagnostics.AddError("Reading field failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *fieldResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state fieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, t := state.ID.ValueString(), state.Type.ValueString()
	want, err := choicesOf(ctx, plan.Choices)
	if err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	have, err := choicesOf(ctx, state.Choices)
	if err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	if err := validateField(plan, want); err != nil {
		resp.Diagnostics.AddError("Invalid field", err.Error())
		return
	}

	// 1. the field itself
	patch := map[string]any{"type": t}
	changed := false
	if !plan.Name.Equal(state.Name) {
		patch["name"], changed = plan.Name.ValueString(), true
	}
	if !plan.HelperText.Equal(state.HelperText) {
		patch["helper_text"], changed = plan.HelperText.ValueString(), true // "" clears it
	}
	if !plan.IsActive.Equal(state.IsActive) {
		patch["is_active"], changed = plan.IsActive.ValueBool(), true
	}
	if hasMultiSelect(t) && !plan.IsMultiSelect.IsUnknown() && !plan.IsMultiSelect.Equal(state.IsMultiSelect) {
		patch["is_multi_select"], changed = plan.IsMultiSelect.ValueBool(), true
	}
	if !plan.HasThousandSeparator.IsUnknown() && !plan.HasThousandSeparator.Equal(state.HasThousandSeparator) {
		patch["has_thousand_separator"], changed = plan.HasThousandSeparator.ValueBool(), true
	}
	if !plan.DecimalPlaces.IsUnknown() && !plan.DecimalPlaces.Equal(state.DecimalPlaces) {
		patch["decimal_places"], changed = plan.DecimalPlaces.ValueInt64(), true
	}
	if !plan.CurrencyCode.IsUnknown() && !plan.CurrencyCode.Equal(state.CurrencyCode) && !plan.CurrencyCode.IsNull() {
		patch["currency_code"], changed = plan.CurrencyCode.ValueString(), true
	}
	if changed {
		if err := r.c.UpdateField(ctx, id, patch); err != nil {
			resp.Diagnostics.AddError("Updating field failed", err.Error())
			return
		}
	}

	// 2. choices: add first (so the last active choice is never removed), then change, then delete
	if hasChoices(t) {
		var add []map[string]any
		for _, k := range sortedKeys(want) {
			if _, ok := have[k]; !ok {
				add = append(add, choicePayload(want[k]))
			}
		}
		if len(add) > 0 {
			if err := r.c.AddFieldChoices(ctx, id, add); err != nil {
				resp.Diagnostics.AddError("Adding choices failed", err.Error())
				return
			}
		}
		for _, k := range sortedKeys(want) {
			old, ok := have[k]
			if !ok {
				continue
			}
			c := want[k]
			p := map[string]any{}
			if !c.Name.Equal(old.Name) {
				p["name"] = c.Name.ValueString()
			}
			if !c.Color.Equal(old.Color) && !c.Color.IsNull() {
				p["color"] = c.Color.ValueString()
			}
			if !c.IsActive.Equal(old.IsActive) {
				p["is_active"] = c.IsActive.ValueBool()
			}
			if len(p) > 0 {
				if err := r.c.UpdateFieldChoice(ctx, id, old.ID.ValueString(), p); err != nil {
					resp.Diagnostics.AddError("Updating choice "+k+" failed", err.Error())
					return
				}
			}
			c.ID = old.ID
			want[k] = c
		}
		for _, k := range sortedKeys(have) {
			if _, ok := want[k]; !ok {
				if err := r.c.DeleteFieldChoice(ctx, id, have[k].ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
					resp.Diagnostics.AddError("Deleting choice "+k+" failed", err.Error())
					return
				}
			}
		}
	}

	f, err := r.c.GetField(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Reading field failed", err.Error())
		return
	}
	if hasChoices(t) {
		if err := assignChoiceIDs(f, want); err != nil {
			resp.Diagnostics.AddError("Reading the field's choices failed", err.Error())
			return
		}
		if plan.Choices, err = choicesToMap(ctx, want); err != nil {
			resp.Diagnostics.AddError("Reading the field's choices failed", err.Error())
			return
		}
	}
	plan.ID = state.ID
	if err := r.refresh(ctx, f, &plan); err != nil {
		resp.Diagnostics.AddError("Reading field failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete deactivates the field: the API has no delete endpoint.
func (r *fieldResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fieldModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.UpdateField(ctx, state.ID.ValueString(), map[string]any{"type": state.Type.ValueString(), "is_active": false})
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Deactivating field failed", err.Error())
		return
	}
	resp.Diagnostics.AddWarning("Field deactivated, not deleted",
		fmt.Sprintf("The CMP API has no delete for custom fields, so %q was deactivated and removed from state.", state.Name.ValueString()))
}

// ImportState reads the field; its choices are not tracked until they are added to the configuration, because
// they need keys. After importing, add a choices block whose names match, and Terraform will adopt them.
func (r *fieldResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// ModifyPlan sets each choice's ID in the plan: the existing ID for a choice already in state, and unknown for a
// new one. The framework leaves a new map element's computed id null, which would make the ID we learn after
// creating the choice look like an inconsistent result.
func (r *fieldResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create (handled by the framework) or destroy
	}
	var plan, state fieldModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.Choices.IsNull() || plan.Choices.IsUnknown() {
		return
	}
	want, err := choicesOf(ctx, plan.Choices)
	if err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	have, err := choicesOf(ctx, state.Choices)
	if err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	for k, c := range want {
		if old, ok := have[k]; ok {
			c.ID = old.ID
		} else {
			c.ID = types.StringUnknown()
		}
		want[k] = c
	}
	if plan.Choices, err = choicesToMap(ctx, want); err != nil {
		resp.Diagnostics.AddError("Invalid choices", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}
