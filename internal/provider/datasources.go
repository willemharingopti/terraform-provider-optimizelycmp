package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

// lookupSpec describes a read-only "find one object by a key" data source.
type lookupSpec struct {
	typeSuffix string // e.g. "team" -> optimizelycmp_team
	noun       string // e.g. "team"
	keyAttr    string // the attribute users search by
	keyDesc    string
	// extras are computed outputs besides id; the value kinds are string or bool.
	extras map[string]any // attribute name -> "string" | "bool" | "list" (list of strings)
	find   func(ctx context.Context, c *client.Client, key string) (id string, extras map[string]any, err error)
}

func eqFold(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

func lookupSpecs() []lookupSpec {
	return []lookupSpec{
		{
			typeSuffix: "user", noun: "user", keyAttr: "email", keyDesc: "Email address of the user.",
			extras: map[string]any{"first_name": "string", "last_name": "string", "full_name": "string"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				u, err := c.FindUserByEmail(ctx, key)
				if err != nil {
					return "", nil, err
				}
				return u.ID, map[string]any{"first_name": u.FirstName, "last_name": u.LastName, "full_name": u.FullName}, nil
			},
		},
		{
			typeSuffix: "team", noun: "team", keyAttr: "name", keyDesc: "Team name (case-insensitive exact match).",
			extras: map[string]any{"user_ids": "list", "user_emails": "list"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListTeams(ctx)
				if err != nil {
					return "", nil, err
				}
				var ids []string
				for _, t := range all {
					if eqFold(t.Name, key) {
						ids = append(ids, t.ID)
					}
				}
				id, _, err := one(ids, key)
				if err != nil {
					return "", nil, err
				}
				t, err := c.GetTeam(ctx, id)
				if err != nil {
					return "", nil, err
				}
				uids, emails := []string{}, []string{}
				for _, u := range t.Users {
					uids = append(uids, u.ID)
					emails = append(emails, u.Email)
				}
				return id, map[string]any{"user_ids": uids, "user_emails": emails}, nil
			},
		},
		{
			typeSuffix: "workflow", noun: "workflow", keyAttr: "name", keyDesc: "Workflow name (case-insensitive exact match).",
			extras: map[string]any{"description": "string", "is_active": "bool", "is_flexible": "bool", "is_smart_duration_enabled": "bool",
				"is_asset_approval_enabled": "bool", "is_resource_management_enabled": "bool", "step_names": "list"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListWorkflows(ctx)
				if err != nil {
					return "", nil, err
				}
				var ids []string
				for _, w := range all {
					if eqFold(w.Name, key) {
						ids = append(ids, w.ID)
					}
				}
				id, _, err := one(ids, key)
				if err != nil {
					return "", nil, err
				}
				w, err := c.GetWorkflow(ctx, id)
				if err != nil {
					return "", nil, err
				}
				desc := ""
				if w.Description != nil {
					desc = *w.Description
				}
				steps := []string{}
				for _, st := range w.Steps {
					steps = append(steps, st.Name)
				}
				return id, map[string]any{"description": desc, "is_active": w.IsActive, "is_flexible": w.IsFlexible,
					"is_smart_duration_enabled": w.IsSmartDurationEnabled, "is_asset_approval_enabled": w.IsAssetApprovalEnabled,
					"is_resource_management_enabled": w.IsResourceManagementEnabled, "step_names": steps}, nil
			},
		},
		{
			typeSuffix: "template", noun: "template", keyAttr: "title", keyDesc: "Template title (case-insensitive exact match).",
			extras: map[string]any{"is_active": "bool", "description": "string", "applicable_to": "list"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListTemplates(ctx, key)
				if err != nil {
					return "", nil, err
				}
				var ids []string
				for _, t := range all {
					if eqFold(t.Title, key) {
						ids = append(ids, t.ID)
					}
				}
				id, _, err := one(ids, key)
				if err != nil {
					return "", nil, err
				}
				t, err := c.GetTemplate(ctx, id)
				if err != nil {
					return "", nil, err
				}
				at := t.ApplicableTo
				if at == nil {
					at = []string{}
				}
				return id, map[string]any{"is_active": t.IsActive, "description": t.Description, "applicable_to": at}, nil
			},
		},
		{
			typeSuffix: "content_type", noun: "content type", keyAttr: "name", keyDesc: "Content type name (case-insensitive exact match).",
			extras: map[string]any{"component": "bool", "disabled": "bool"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListContentTypes(ctx)
				if err != nil {
					return "", nil, err
				}
				var hits []client.ContentType
				for _, t := range all {
					if eqFold(t.Name, key) {
						hits = append(hits, t)
					}
				}
				if len(hits) != 1 {
					return one(nil, key, len(hits))
				}
				return hits[0].GUID, map[string]any{"component": hits[0].Component, "disabled": hits[0].Disabled}, nil
			},
		},
		{
			typeSuffix: "field", noun: "custom field", keyAttr: "name", keyDesc: "Field name (case-insensitive exact match).",
			extras: map[string]any{"type": "string", "is_active": "bool", "helper_text": "string"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListFields(ctx)
				if err != nil {
					return "", nil, err
				}
				var hits []client.Field
				for _, f := range all {
					if eqFold(f.Name, key) {
						hits = append(hits, f)
					}
				}
				if len(hits) != 1 {
					return one(nil, key, len(hits))
				}
				help := ""
				if hits[0].HelperText != nil {
					help = *hits[0].HelperText
				}
				return hits[0].ID, map[string]any{"type": hits[0].Type, "is_active": hits[0].IsActive, "helper_text": help}, nil
			},
		},
		{
			typeSuffix: "label_group", noun: "label group", keyAttr: "name", keyDesc: "Label group name (case-insensitive exact match).",
			extras: map[string]any{"source_org_type": "string", "value_names": "list"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListLabelGroups(ctx)
				if err != nil {
					return "", nil, err
				}
				var hits []client.LabelGroup
				for _, g := range all {
					if eqFold(g.Name, key) {
						hits = append(hits, g)
					}
				}
				if len(hits) != 1 {
					return one(nil, key, len(hits))
				}
				names := []string{}
				for _, v := range hits[0].Values {
					names = append(names, v.Name)
				}
				return hits[0].ID, map[string]any{"source_org_type": hits[0].SourceOrgType, "value_names": names}, nil
			},
		},
		{
			typeSuffix: "brand_compliance_category", noun: "brand compliance category", keyAttr: "name", keyDesc: "Category name (case-insensitive exact match).",
			extras: map[string]any{"criteria_names": "list"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListBrandComplianceCategories(ctx)
				if err != nil {
					return "", nil, err
				}
				var hits []client.BrandComplianceCategory
				for _, g := range all {
					if eqFold(g.Name, key) {
						hits = append(hits, g)
					}
				}
				if len(hits) != 1 {
					return one(nil, key, len(hits))
				}
				names := []string{}
				for _, cr := range hits[0].Criteria {
					names = append(names, cr.Name)
				}
				return hits[0].ID, map[string]any{"criteria_names": names}, nil
			},
		},
		{
			typeSuffix: "publishing_channel", noun: "publishing channel", keyAttr: "name", keyDesc: "Channel name (case-insensitive exact match).",
			extras: map[string]any{"disabled": "bool"},
			find: func(ctx context.Context, c *client.Client, key string) (string, map[string]any, error) {
				all, err := c.ListPublishingChannels(ctx)
				if err != nil {
					return "", nil, err
				}
				var hits []client.PublishingChannel
				for _, ch := range all {
					if eqFold(ch.Name, key) {
						hits = append(hits, ch)
					}
				}
				if len(hits) != 1 {
					return one(nil, key, len(hits))
				}
				return hits[0].ID, map[string]any{"disabled": hits[0].Disabled}, nil
			},
		},
	}
}

// one turns a list of matching IDs into a single id, or an error for zero or several matches.
func one(ids []string, key string, n ...int) (string, map[string]any, error) {
	count := len(ids)
	if len(n) > 0 {
		count = n[0]
	}
	switch count {
	case 1:
		return ids[0], nil, nil
	case 0:
		return "", nil, fmt.Errorf("nothing found matching %q", key)
	default:
		return "", nil, fmt.Errorf("%d matches for %q; the name must identify exactly one", count, key)
	}
}

type lookupDataSource struct {
	spec lookupSpec
	c    *client.Client
}

var (
	_ datasource.DataSource              = &lookupDataSource{}
	_ datasource.DataSourceWithConfigure = &lookupDataSource{}
)

func (d *lookupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.spec.typeSuffix
}

func (d *lookupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		d.spec.keyAttr: schema.StringAttribute{Required: true, Description: d.spec.keyDesc},
		"id":           schema.StringAttribute{Computed: true, Description: "ID of the " + d.spec.noun + "."},
	}
	for name, kind := range d.spec.extras {
		switch kind {
		case "bool":
			attrs[name] = schema.BoolAttribute{Computed: true}
		case "list":
			attrs[name] = schema.ListAttribute{Computed: true, ElementType: types.StringType}
		default:
			attrs[name] = schema.StringAttribute{Computed: true}
		}
	}
	resp.Schema = schema.Schema{Description: "Looks up one " + d.spec.noun + " by " + d.spec.keyAttr + ". It fails if there is no match or more than one.", Attributes: attrs}
}

func (d *lookupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *lookupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var key string
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(d.spec.keyAttr), &key)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, extras, err := d.spec.find(ctx, d.c, key)
	if err != nil {
		resp.Diagnostics.AddError("Looking up "+d.spec.noun+" failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(d.spec.keyAttr), key)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	for name, v := range extras {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), v)...)
	}
}
