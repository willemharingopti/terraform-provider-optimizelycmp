package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ datasource.DataSource              = &usersDataSource{}
	_ datasource.DataSourceWithConfigure = &usersDataSource{}
)

func NewUsersDataSource() datasource.DataSource { return &usersDataSource{} }

type usersDataSource struct{ c *client.Client }

type usersModel struct {
	ID    types.String `tfsdk:"id"`
	Users types.List   `tfsdk:"users"`
}

var userObjType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"id": types.StringType, "first_name": types.StringType, "last_name": types.StringType,
	"full_name": types.StringType, "roles": types.ListType{ElemType: types.StringType},
}}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Every user in the organisation, with their roles. Use optimizelycmp_user to find one by email.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Always \"users\"; present because every data source needs an ID."},
			"users": schema.ListNestedAttribute{Computed: true, Description: "The users.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true},
					"first_name": schema.StringAttribute{Computed: true},
					"last_name":  schema.StringAttribute{Computed: true},
					"full_name":  schema.StringAttribute{Computed: true},
					"roles":      schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Role names."},
				}}},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *usersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	all, err := d.c.ListUsers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Listing users failed", err.Error())
		return
	}
	elems := make([]attr.Value, 0, len(all))
	for _, u := range all {
		roles := make([]attr.Value, 0, len(u.Roles))
		for _, r := range u.Roles {
			roles = append(roles, types.StringValue(r.Name))
		}
		rl, diags := types.ListValue(types.StringType, roles)
		resp.Diagnostics.Append(diags...)
		o, diags := types.ObjectValue(userObjType.AttrTypes, map[string]attr.Value{
			"id": types.StringValue(u.ID), "first_name": types.StringValue(u.FirstName), "last_name": types.StringValue(u.LastName),
			"full_name": types.StringValue(u.FullName), "roles": rl,
		})
		resp.Diagnostics.Append(diags...)
		elems = append(elems, o)
	}
	list, diags := types.ListValue(userObjType, elems)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, usersModel{ID: types.StringValue("users"), Users: list})...)
}
