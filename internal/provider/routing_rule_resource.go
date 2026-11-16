package provider

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                = &routingRuleResource{}
	_ resource.ResourceWithConfigure   = &routingRuleResource{}
	_ resource.ResourceWithImportState = &routingRuleResource{}
	_ resource.ResourceWithModifyPlan  = &routingRuleResource{}
)

var routingKind = settingsKind{section: "routing_rules", noun: "routing rule"}

func NewRoutingRuleResource() resource.Resource { return &routingRuleResource{} }

type routingRuleResource struct{ c *client.Client }

type routingRuleModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	TemplateName types.String `tfsdk:"template_name"`
	Watchers     types.Set    `tfsdk:"watchers"`
	Rules        types.List   `tfsdk:"rules"`
}

type conditionModel struct {
	FieldType       types.String `tfsdk:"field_type"`
	Identifier      types.String `tfsdk:"identifier"`
	Operator        types.String `tfsdk:"operator"`
	Value           types.List   `tfsdk:"value"`
	Unit            types.String `tfsdk:"unit"`
	CustomFieldType types.String `tfsdk:"custom_field_type"`
}

var conditionObjType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"field_type": types.StringType, "identifier": types.StringType, "operator": types.StringType,
	"value": types.ListType{ElemType: types.StringType}, "unit": types.StringType, "custom_field_type": types.StringType,
}}

func (r *routingRuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_routing_rule"
}

func (r *routingRuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A routing rule, written through the API's bulk settings import (experimental). The import matches by name and never deletes, " +
			"so the name cannot change (a new name is a new rule) and destroying this resource only stops Terraform managing it. " +
			"After every write the rule is read back and the apply fails if CMP did not keep what was sent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The rule name (the API gives routing rules no other identifier).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Rule name. Changing it creates a new rule and leaves the old one in CMP.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"description":   schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "Description."},
			"template_name": schema.StringAttribute{Optional: true, Description: "Name of the template this rule applies. It must name an existing template: the API silently drops an unknown name, and the apply then fails. Unsetting it clears the template."},
			"watchers": schema.SetAttribute{Optional: true, ElementType: types.StringType,
				Description: "Email addresses of CMP users who watch work routed by this rule. They must be existing CMP users: the API silently drops any other address, and the apply then fails. " +
					"CMP also adds a default watcher on its own, and watchers added in CMP outside Terraform are ignored. Watchers cannot be removed through the API as far as tested, so removing one fails at plan time."},
			"rules": schema.ListNestedAttribute{Optional: true,
				Description: "Conditions. The API validates them; when tested it accepted field_type dueDate, label and customField, and operator \"equal\" for label and customField conditions.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"field_type": schema.StringAttribute{Required: true, Description: "dueDate, label or customField.",
						Validators: []validator.String{stringvalidator.OneOf("dueDate", "label", "customField")}},
					"identifier": schema.StringAttribute{Optional: true, Description: "Required for label (the label group) and customField (the field) conditions; must be unset for dueDate."},
					"operator":   schema.StringAttribute{Required: true, Description: "For example equal."},
					"value":      schema.ListAttribute{Required: true, ElementType: types.StringType, Description: "The labels, choices or due date value to compare with."},
					"unit": schema.StringAttribute{Optional: true, Description: "days, weeks, months or years. Required for dueDate and not allowed otherwise.",
						Validators: []validator.String{stringvalidator.OneOf("days", "weeks", "months", "years")}},
					"custom_field_type": schema.StringAttribute{Optional: true, Description: "For customField conditions: dropdown, multichoice, multi_select_dropdown or checkbox."},
				}}},
		},
	}
}

func (r *routingRuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func nilIfNull(v types.String) *string { return optString(v) }

func (m *routingRuleModel) item(ctx context.Context) (client.RoutingRule, error) {
	out := client.RoutingRule{
		Name: m.Name.ValueString(), Description: m.Description.ValueString(), TemplateName: nilIfNull(m.TemplateName),
		Watchers: stringsOf(ctx, m.Watchers), Rules: []client.RoutingCondition{},
	}
	if m.Rules.IsNull() || m.Rules.IsUnknown() {
		return out, nil
	}
	var conds []conditionModel
	if d := m.Rules.ElementsAs(ctx, &conds, false); d.HasError() {
		return out, fmt.Errorf("reading rules: %v", d)
	}
	for _, c := range conds {
		vals := []string{}
		if !c.Value.IsNull() && !c.Value.IsUnknown() {
			_ = c.Value.ElementsAs(ctx, &vals, false)
		}
		out.Rules = append(out.Rules, client.RoutingCondition{
			FieldType: c.FieldType.ValueString(), Identifier: nilIfNull(c.Identifier), Operator: c.Operator.ValueString(),
			Value: vals, Unit: nilIfNull(c.Unit), CustomFieldType: nilIfNull(c.CustomFieldType),
		})
	}
	return out, nil
}

func ptrString(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// refresh reads the rule from the export. It returns the watchers CMP really has, because the state only tracks
// the ones configured.
func (r *routingRuleResource) refresh(ctx context.Context, m *routingRuleModel) (found bool, apiWatchers []string, err error) {
	all, err := r.c.ExportRoutingRules(ctx)
	if err != nil {
		return false, nil, err
	}
	var hits []client.RoutingRule
	for _, x := range all {
		if x.Name == m.Name.ValueString() {
			hits = append(hits, x)
		}
	}
	if err := routingKind.onlyOne(len(hits), m.Name.ValueString()); err != nil || len(hits) == 0 {
		return false, nil, err
	}
	x := hits[0]
	m.ID, m.Name, m.Description = types.StringValue(x.Name), types.StringValue(x.Name), types.StringValue(x.Description)
	m.TemplateName = ptrString(x.TemplateName)
	// CMP adds a default watcher of its own, so track only the watchers that were configured (those still present).
	if m.Watchers.IsNull() || m.Watchers.IsUnknown() {
		m.Watchers = types.SetNull(types.StringType)
	} else {
		var kept []string
		for _, w := range stringsOf(ctx, m.Watchers) {
			if inList(x.Watchers, w) {
				kept = append(kept, w)
			}
		}
		m.Watchers = stringSetOf(kept)
	}
	if len(x.Rules) == 0 && m.Rules.IsNull() {
		return true, x.Watchers, nil
	}
	elems := make([]attr.Value, 0, len(x.Rules))
	for _, c := range x.Rules {
		vals := make([]attr.Value, len(c.Value))
		for i, v := range c.Value {
			vals[i] = types.StringValue(v)
		}
		vl, d := types.ListValue(types.StringType, vals)
		if d.HasError() {
			return false, nil, fmt.Errorf("%v", d)
		}
		o, d := types.ObjectValue(conditionObjType.AttrTypes, map[string]attr.Value{
			"field_type": types.StringValue(c.FieldType), "identifier": ptrString(c.Identifier), "operator": types.StringValue(c.Operator),
			"value": vl, "unit": ptrString(c.Unit), "custom_field_type": ptrString(c.CustomFieldType),
		})
		if d.HasError() {
			return false, nil, fmt.Errorf("%v", d)
		}
		elems = append(elems, o)
	}
	var d diag.Diagnostics
	m.Rules, d = types.ListValue(conditionObjType, elems)
	if d.HasError() {
		return false, nil, fmt.Errorf("%v", d)
	}
	return true, x.Watchers, nil
}

func (r *routingRuleResource) finish(ctx context.Context, plan *routingRuleModel, sent client.RoutingRule, st *tfsdk.State, diags *diag.Diagnostics) {
	found, apiWatchers, err := r.refresh(ctx, plan)
	if err != nil || !found {
		diags.AddError("Reading the routing rule back failed", fmt.Sprintf("found=%v err=%v", found, err))
		return
	}
	diags.Append(st.Set(ctx, plan)...)
	got, err := plan.item(ctx)
	if err != nil {
		diags.AddError("Reading the routing rule back failed", err.Error())
		return
	}
	norm := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	var problems []string
	if got.Description != sent.Description {
		problems = append(problems, "the description differs")
	}
	if norm(got.TemplateName) != norm(sent.TemplateName) {
		problems = append(problems, fmt.Sprintf("template_name %q was not kept (CMP silently drops a template that does not exist)", norm(sent.TemplateName)))
	}
	if missing := removedFrom(sent.Watchers, apiWatchers); len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("watchers %v were not kept (CMP silently drops an address that is not a CMP user)", missing))
	}
	if !reflect.DeepEqual(got.Rules, sent.Rules) {
		problems = append(problems, "the rules differ")
	}
	if len(problems) > 0 {
		diags.AddError("CMP did not keep the routing rule as requested",
			"After writing, the rule in CMP differs from the configuration: "+strings.Join(problems, "; ")+". The state now shows what CMP has.")
	}
}

// ModifyPlan refuses to drop a watcher: the API was not seen removing any, so the apply could not do what the plan promises.
func (r *routingRuleResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state routingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || plan.Watchers.IsUnknown() {
		return
	}
	if gone := removedFrom(stringsOf(ctx, state.Watchers), stringsOf(ctx, plan.Watchers)); len(gone) > 0 {
		resp.Diagnostics.AddAttributeError(path.Root("watchers"), "Watchers cannot be removed",
			fmt.Sprintf("Removing %s would not take effect: the CMP API was not seen removing a watcher. Keep it in the configuration, or remove it in CMP and then here.",
				strings.Join(gone, ", ")))
	}
}

func (r *routingRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan routingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item, err := plan.item(ctx)
	if err == nil {
		err = routingKind.guardCreate(ctx, r.c, item.Name, item)
	}
	if err == nil {
		err = routingKind.write(ctx, r.c, item)
	}
	if err != nil {
		resp.Diagnostics.AddError("Creating routing rule failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

func (r *routingRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan routingRuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	item, err := plan.item(ctx)
	if err == nil {
		err = routingKind.guardUpdate(ctx, r.c, item.Name, item)
	}
	if err == nil {
		err = routingKind.write(ctx, r.c, item)
	}
	if err != nil {
		resp.Diagnostics.AddError("Updating routing rule failed", err.Error())
		return
	}
	r.finish(ctx, &plan, item, &resp.State, &resp.Diagnostics)
}

func (r *routingRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state routingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, _, err := r.refresh(ctx, &state)
	if err != nil {
		resp.Diagnostics.AddError("Reading routing rule failed", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete only forgets the rule: the API cannot delete routing rules.
func (r *routingRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state routingRuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.AddWarning("Routing rule not deleted",
		fmt.Sprintf("The CMP API cannot delete routing rules, so %q was only removed from Terraform state and still exists in CMP.", state.Name.ValueString()))
}

// ImportState takes the rule's name.
func (r *routingRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
