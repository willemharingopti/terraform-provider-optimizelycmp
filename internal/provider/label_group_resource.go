package provider

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &labelGroupResource{}
	_ resource.ResourceWithConfigure   = &labelGroupResource{}
	_ resource.ResourceWithImportState = &labelGroupResource{}
	_ resource.ResourceWithModifyPlan  = &labelGroupResource{}
)

// labelColors are the colours the API accepted for labels when tested. Its documentation lists a ninth, #702BD5,
// which it rejects; hex is case-sensitive and other values are refused.
var labelColors = []string{"#4ECFD5", "#FFC700", "#FF98A7", "#5FEEAD", "#D6C4F2", "#5F9FF6", "#CB5DEB", "#9694B3"}

func NewLabelGroupResource() resource.Resource { return &labelGroupResource{} }

type labelGroupResource struct{ c *client.Client }

type labelGroupModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	HasSingleValue types.Bool   `tfsdk:"has_single_value"`
	Labels         types.Map    `tfsdk:"labels"`
}

type labelModel struct {
	Color types.String `tfsdk:"color"`
}

var labelObjType = types.ObjectType{AttrTypes: map[string]attr.Type{"color": types.StringType}}

func (r *labelGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_label_group"
}

func (r *labelGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A label group and its labels, written through the API's bulk settings import (experimental). " +
			"The import only **adds** labels and never deletes anything, so: labels cannot be removed (the plan fails if you try), " +
			"the name cannot change (a new name is a new group), and destroying this resource only stops Terraform managing the group. " +
			"Use the group's display name. To manage a group created in the CMP interface, import it by that name.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Label group ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Label group name. Changing it creates a new group and leaves the old one in CMP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"has_single_value": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "true if only one label of the group can be chosen, false to allow several."},
			"labels": schema.MapNestedAttribute{
				Required:    true,
				Description: "Labels, keyed by label name. Adding a key adds the label; changing a colour overwrites it. Labels added in CMP outside Terraform are ignored.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"color": schema.StringAttribute{Required: true,
						Description: "Label colour: one of " + strings.Join(labelColors, ", ") + " (upper-case). The API's documentation also lists #702BD5, but it rejects it.",
						Validators:  []validator.String{stringvalidator.OneOf(labelColors...)}},
				}},
			},
		},
	}
}

func (r *labelGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func labelsOf(ctx context.Context, m types.Map) (map[string]labelModel, error) {
	out := map[string]labelModel{}
	if m.IsNull() || m.IsUnknown() {
		return out, nil
	}
	if d := m.ElementsAs(ctx, &out, false); d.HasError() {
		return nil, fmt.Errorf("reading labels: %v", d)
	}
	return out, nil
}

func labelsToMap(ctx context.Context, in map[string]labelModel) (types.Map, error) {
	v, d := types.MapValueFrom(ctx, labelObjType, in)
	if d.HasError() {
		return types.MapNull(labelObjType), fmt.Errorf("building labels: %v", d)
	}
	return v, nil
}

func clientLabels(in map[string]labelModel) []client.Label {
	names := make([]string, 0, len(in))
	for n := range in {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]client.Label, 0, len(in))
	for _, n := range names {
		out = append(out, client.Label{Name: n, Color: in[n].Color.ValueString()})
	}
	return out
}

func inList(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// slugOf is how the export spells a label group created in the CMP interface ("Content Pillar" -> "content_pillar").
func slugOf(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}

// writeName is the name to send the API for this group. The API identifies a group by its label_type, which for a group
// created in the CMP interface is a slug of the display name ("Content Pillar" is "content_pillar"). Writing the display
// name would not match that group: it would create a second one. So use the export's own spelling when it has one.
func (r *labelGroupResource) writeName(ctx context.Context, name string) (string, error) {
	exports, err := r.c.ExportLabelGroups(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range exports {
		if e.LabelType == name {
			return name, nil
		}
	}
	for _, e := range exports {
		if e.LabelType == slugOf(name) {
			return e.LabelType, nil
		}
	}
	return name, nil
}

// ModifyPlan refuses to remove a label: the API can only add them, so the apply could not do what the plan promises.
func (r *labelGroupResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state labelGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.Labels.IsUnknown() {
		return
	}
	want, err := labelsOf(ctx, plan.Labels)
	if err != nil {
		return
	}
	have, _ := labelsOf(ctx, state.Labels)
	var removed []string
	for n := range have {
		if _, ok := want[n]; !ok {
			removed = append(removed, n)
		}
	}
	if len(removed) > 0 {
		sort.Strings(removed)
		resp.Diagnostics.AddAttributeError(path.Root("labels"), "Labels cannot be removed",
			fmt.Sprintf("The CMP API can only add labels to a label group, never remove them, so removing %s would not take effect. "+
				"Keep the label in the configuration, or delete it in CMP and then remove it here.", strings.Join(removed, ", ")))
	}
}

// refresh reads the group from the list endpoint (ID, name, label names) and the export (colours).
func (r *labelGroupResource) refresh(ctx context.Context, m *labelGroupModel, byID bool) (found bool, err error) {
	groups, err := r.c.ListLabelGroups(ctx)
	if err != nil {
		return false, err
	}
	var g *client.LabelGroup
	for i := range groups {
		match := groups[i].Name == m.Name.ValueString()
		if byID {
			match = groups[i].ID == m.ID.ValueString()
		}
		if match && groups[i].SourceOrgType != "related" {
			g = &groups[i]
			break
		}
	}
	if g == nil {
		return false, nil
	}
	m.ID, m.Name = types.StringValue(g.ID), types.StringValue(g.Name)
	exports, err := r.c.ExportLabelGroups(ctx)
	if err != nil {
		return false, err
	}
	colors := map[string]string{}
	for _, e := range exports {
		if e.LabelType == g.Name || e.LabelType == slugOf(g.Name) {
			m.HasSingleValue = types.BoolValue(e.HasSingleValue)
			for _, l := range e.Labels {
				colors[l.Name] = l.Color
			}
			break
		}
	}
	tracked, err := labelsOf(ctx, m.Labels)
	if err != nil {
		return false, err
	}
	present := map[string]bool{}
	for _, v := range g.Values {
		present[v.Name] = true
	}
	out := map[string]labelModel{}
	if m.Labels.IsNull() { // import: adopt every label
		for _, v := range g.Values {
			c := types.StringNull()
			if col, ok := colors[v.Name]; ok {
				c = types.StringValue(col)
			}
			out[v.Name] = labelModel{Color: c}
		}
	} else {
		for n, old := range tracked {
			if !present[n] {
				continue
			}
			c := old.Color
			if col, ok := colors[n]; ok {
				c = types.StringValue(col)
			}
			out[n] = labelModel{Color: c}
		}
	}
	m.Labels, err = labelsToMap(ctx, out)
	return true, err
}

func (r *labelGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan labelGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	labels, err := labelsOf(ctx, plan.Labels)
	if err != nil {
		resp.Diagnostics.AddError("Invalid labels", err.Error())
		return
	}
	name := plan.Name.ValueString()
	existsErr := func() {
		resp.Diagnostics.AddError("Label group already exists",
			fmt.Sprintf("A label group named %q already exists. Import it with `terraform import <address> %q` instead of creating it.", name, name))
	}
	// The import updates a group of the same name instead of failing, and the API may know a group by a different
	// spelling than its display name, so check both the list and a dry run before writing anything.
	groups, err := r.c.ListLabelGroups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Checking the label group failed", err.Error())
		return
	}
	for _, g := range groups {
		if g.SourceOrgType != "related" && (g.Name == name || slugOf(g.Name) == name) {
			existsErr()
			return
		}
	}
	cs, err := r.c.UpsertLabelGroup(ctx, name, plan.HasSingleValue.ValueBool(), clientLabels(labels), false)
	if err != nil {
		resp.Diagnostics.AddError("Checking the label group failed", err.Error())
		return
	}
	if !inList(cs.Create["labels"], name) { // anything that is not new already exists, even if nothing would change
		existsErr()
		return
	}
	if _, err := r.c.UpsertLabelGroup(ctx, name, plan.HasSingleValue.ValueBool(), clientLabels(labels), true); err != nil {
		resp.Diagnostics.AddError("Creating label group failed", err.Error())
		return
	}
	found, err := r.refresh(ctx, &plan, false)
	if err != nil || !found {
		resp.Diagnostics.AddError("Reading the new label group failed", fmt.Sprintf("found=%v err=%v", found, err))
		return
	}
	plan.Labels = appliedLabels(ctx, labels)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// appliedLabels makes sure the state keeps exactly the labels and colours that were applied.
func appliedLabels(ctx context.Context, applied map[string]labelModel) types.Map {
	m, err := labelsToMap(ctx, applied)
	if err != nil {
		return types.MapNull(labelObjType)
	}
	return m
}

func (r *labelGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state labelGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.refresh(ctx, &state, true)
	if err != nil {
		resp.Diagnostics.AddError("Reading label group failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *labelGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state labelGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	labels, err := labelsOf(ctx, plan.Labels)
	if err != nil {
		resp.Diagnostics.AddError("Invalid labels", err.Error())
		return
	}
	target, err := r.writeName(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Updating label group failed", err.Error())
		return
	}
	// Refuse to write if the API would not treat it as the existing group: that would create a duplicate.
	cs, err := r.c.UpsertLabelGroup(ctx, target, plan.HasSingleValue.ValueBool(), clientLabels(labels), false)
	if err != nil {
		resp.Diagnostics.AddError("Updating label group failed", err.Error())
		return
	}
	if inList(cs.Create["labels"], target) {
		resp.Diagnostics.AddError("Updating label group would create a duplicate",
			fmt.Sprintf("The API does not recognise %q as the existing group and would create a new one. Nothing was changed.", target))
		return
	}
	if _, err := r.c.UpsertLabelGroup(ctx, target, plan.HasSingleValue.ValueBool(), clientLabels(labels), true); err != nil {
		resp.Diagnostics.AddError("Updating label group failed", err.Error())
		return
	}
	plan.ID = state.ID
	found, err := r.refresh(ctx, &plan, true)
	if err != nil || !found {
		resp.Diagnostics.AddError("Reading the label group failed", fmt.Sprintf("found=%v err=%v", found, err))
		return
	}
	plan.Labels = appliedLabels(ctx, labels)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete only forgets the group: the API cannot delete label groups.
func (r *labelGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state labelGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.AddWarning("Label group not deleted",
		fmt.Sprintf("The CMP API cannot delete label groups, so %q was only removed from Terraform state and still exists in CMP.", state.Name.ValueString()))
}

// ImportState takes the label group's name.
func (r *labelGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groups, err := r.c.ListLabelGroups(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing label groups failed", err.Error())
		return
	}
	for _, g := range groups {
		if g.Name == req.ID && g.SourceOrgType != "related" {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), g.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), g.Name)...)
			return
		}
	}
	resp.Diagnostics.AddError("Label group not found", fmt.Sprintf("No label group is named %q. Use its exact display name.", req.ID))
}
