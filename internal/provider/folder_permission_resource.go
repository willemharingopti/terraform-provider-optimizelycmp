package provider

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &folderPermissionResource{}
	_ resource.ResourceWithConfigure   = &folderPermissionResource{}
	_ resource.ResourceWithImportState = &folderPermissionResource{}
)

func NewFolderPermissionResource() resource.Resource { return &folderPermissionResource{} }

type folderPermissionResource struct{ c *client.Client }

type folderPermissionModel struct {
	ID           types.String `tfsdk:"id"`
	FolderID     types.String `tfsdk:"folder_id"`
	AccessorID   types.String `tfsdk:"accessor_id"`
	AccessorType types.String `tfsdk:"accessor_type"`
	AccessType   types.String `tfsdk:"access_type"`
	IsOwner      types.Bool   `tfsdk:"is_owner"`
}

func (r *folderPermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder_permission"
}

func (r *folderPermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Grants one user or team access to a folder. Destroying it revokes that access.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, Description: "<folder_id>/<accessor_id>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"folder_id":   schema.StringAttribute{Required: true, Description: "Folder ID.", PlanModifiers: replace},
			"accessor_id": schema.StringAttribute{Required: true, Description: "ID of the user or team.", PlanModifiers: replace},
			"accessor_type": schema.StringAttribute{Required: true, Description: "user or team.", PlanModifiers: replace,
				Validators: []validator.String{stringvalidator.OneOf("user", "team")}},
			"access_type": schema.StringAttribute{Required: true, Description: "view or edit.",
				Validators: []validator.String{stringvalidator.OneOf("view", "edit")}},
			"is_owner": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Make the accessor the folder owner (users only). The API can only set this to true: ownership moves by making another user owner, so changing true back to false is refused."},
		},
	}
}

func (r *folderPermissionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *folderPermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan folderPermissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.IsOwner.ValueBool() && plan.AccessorType.ValueString() == "team" {
		resp.Diagnostics.AddAttributeError(path.Root("is_owner"), "A team cannot be owner", "Only a user can own a folder.")
		return
	}
	err := r.c.GrantFolderPermission(ctx, plan.FolderID.ValueString(), plan.AccessorID.ValueString(),
		plan.AccessorType.ValueString(), plan.AccessType.ValueString(), plan.IsOwner.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Granting folder permission failed", err.Error())
		return
	}
	plan.ID = types.StringValue(plan.FolderID.ValueString() + "/" + plan.AccessorID.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *folderPermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state folderPermissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.c.GetFolderPermission(ctx, state.FolderID.ValueString(), state.AccessorID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading folder permission failed", err.Error())
		return
	}
	state.AccessorType = types.StringValue(p.Type)
	state.AccessType = types.StringValue(p.AccessType)
	state.IsOwner = types.BoolValue(p.IsOwner)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *folderPermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state folderPermissionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if state.IsOwner.ValueBool() && !plan.IsOwner.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("is_owner"), "Ownership cannot be removed",
			"The API only lets an accessor be set as owner. Make another user the owner instead, then this accessor stops being the owner.")
		return
	}
	err := r.c.UpdateFolderPermission(ctx, plan.FolderID.ValueString(), plan.AccessorID.ValueString(),
		plan.AccessType.ValueString(), plan.IsOwner.ValueBool() && !state.IsOwner.ValueBool())
	if err != nil {
		resp.Diagnostics.AddError("Updating folder permission failed", err.Error())
		return
	}
	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *folderPermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state folderPermissionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.RevokeFolderPermission(ctx, state.FolderID.ValueString(), state.AccessorID.ValueString())
	if err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Revoking folder permission failed", err.Error())
	}
}

// ImportState accepts <folder_id>/<accessor_id>; the remaining attributes are read from the API.
func (r *folderPermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	folder, accessor, ok := strings.Cut(req.ID, "/")
	if !ok || folder == "" || accessor == "" {
		resp.Diagnostics.AddError("Invalid import ID", "Use <folder_id>/<accessor_id>.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("folder_id"), folder)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("accessor_id"), accessor)...)
}
