package provider

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var (
	_ resource.Resource                = &folderResource{}
	_ resource.ResourceWithConfigure   = &folderResource{}
	_ resource.ResourceWithImportState = &folderResource{}
)

func NewFolderResource() resource.Resource { return &folderResource{} }

type folderResource struct{ c *client.Client }

type folderModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ParentFolderID types.String `tfsdk:"parent_folder_id"`
	Path           types.String `tfsdk:"path"`
	CreatedAt      types.String `tfsdk:"created_at"`
	ModifiedAt     types.String `tfsdk:"modified_at"`
}

func (r *folderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_folder"
}

func (r *folderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A folder in the CMP asset library. Destroying it deletes the folder through the API.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Folder ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "Folder name."},
			"parent_folder_id": schema.StringAttribute{Optional: true,
				Description: "ID of the parent folder. Omit to place the folder at the root; removing it later moves the folder back to the root."},
			"path":        schema.StringAttribute{Computed: true, Description: "Folder location, e.g. /images/icons."},
			"created_at":  schema.StringAttribute{Computed: true, Description: "Creation time (ISO 8601 UTC)."},
			"modified_at": schema.StringAttribute{Computed: true, Description: "Last modification time (ISO 8601 UTC)."},
		},
	}
}

func (r *folderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func optString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func fromFolder(f *client.Folder, m *folderModel) {
	m.ID = types.StringValue(f.ID)
	m.Name = types.StringValue(f.Name)
	m.Path = types.StringValue(f.Path)
	m.CreatedAt = types.StringValue(f.CreatedAt)
	m.ModifiedAt = types.StringValue(f.ModifiedAt)
	if f.ParentFolderID == nil || *f.ParentFolderID == "" {
		m.ParentFolderID = types.StringNull()
	} else {
		m.ParentFolderID = types.StringValue(*f.ParentFolderID)
	}
}

func (r *folderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan folderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.CreateFolder(ctx, plan.Name.ValueString(), optString(plan.ParentFolderID))
	if err != nil {
		resp.Diagnostics.AddError("Creating folder failed", err.Error())
		return
	}
	fromFolder(f, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *folderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state folderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.GetFolder(ctx, state.ID.ValueString())
	if errors.Is(err, client.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading folder failed", err.Error())
		return
	}
	fromFolder(f, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *folderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state folderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	f, err := r.c.UpdateFolder(ctx, state.ID.ValueString(), plan.Name.ValueString(), optString(plan.ParentFolderID))
	if err != nil {
		resp.Diagnostics.AddError("Updating folder failed", err.Error())
		return
	}
	fromFolder(f, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *folderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state folderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.DeleteFolder(ctx, state.ID.ValueString()); err != nil && !errors.Is(err, client.ErrNotFound) {
		resp.Diagnostics.AddError("Deleting folder failed", err.Error())
	}
}

func (r *folderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
