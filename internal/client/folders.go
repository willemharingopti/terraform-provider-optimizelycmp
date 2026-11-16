package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// Folder is a DAM library folder.
type Folder struct {
	ID             string  `json:"id,omitempty"`
	Name           string  `json:"name,omitempty"`
	ParentFolderID *string `json:"parent_folder_id"`
	Path           string  `json:"path,omitempty"`
	CreatedAt      string  `json:"created_at,omitempty"`
	ModifiedAt     string  `json:"modified_at,omitempty"`
}

type folderCreate struct {
	Name           string  `json:"name"`
	ParentFolderID *string `json:"parent_folder_id"`
}

func folderPath(id string) string { return "/folders/" + url.PathEscape(id) }

var errEmptyID = errors.New("refusing request with an empty id")

func (c *Client) CreateFolder(ctx context.Context, name string, parentID *string) (*Folder, error) {
	var out Folder
	if err := c.Do(ctx, http.MethodPost, "/folders", folderCreate{Name: name, ParentFolderID: parentID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetFolder(ctx context.Context, id string) (*Folder, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out Folder
	if err := c.Do(ctx, http.MethodGet, folderPath(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateFolder sets name and parent. A nil parentID moves the folder to the root.
func (c *Client) UpdateFolder(ctx context.Context, id, name string, parentID *string) (*Folder, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out Folder
	if err := c.Do(ctx, http.MethodPatch, folderPath(id), folderCreate{Name: name, ParentFolderID: parentID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteFolder(ctx context.Context, id string) error {
	if id == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodDelete, folderPath(id), nil, nil)
}

// ListFolders lists the folders under parentID (the root level when empty).
func (c *Client) ListFolders(ctx context.Context, parentID string) ([]Folder, error) {
	path := "/folders"
	if parentID != "" {
		path += "?parent_folder_id=" + url.QueryEscape(parentID)
	}
	return listAll[Folder](ctx, c, path)
}

// FolderPermission is one accessor's access to a folder.
type FolderPermission struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	IsOwner    bool   `json:"is_owner"`
	AccessType string `json:"access_type"`
}

func permPath(folderID, accessorID string) string {
	return folderPath(folderID) + "/permissions/" + url.PathEscape(accessorID)
}

func (c *Client) ListFolderPermissions(ctx context.Context, folderID string) ([]FolderPermission, error) {
	if folderID == "" {
		return nil, errEmptyID
	}
	return listAll[FolderPermission](ctx, c, folderPath(folderID)+"/permissions")
}

// GetFolderPermission returns the accessor's permission or ErrNotFound.
func (c *Client) GetFolderPermission(ctx context.Context, folderID, accessorID string) (*FolderPermission, error) {
	perms, err := c.ListFolderPermissions(ctx, folderID)
	if err != nil {
		return nil, err
	}
	for i := range perms {
		if perms[i].ID == accessorID {
			return &perms[i], nil
		}
	}
	return nil, ErrNotFound
}

func (c *Client) GrantFolderPermission(ctx context.Context, folderID, accessorID, accessorType, accessType string, isOwner bool) error {
	if folderID == "" || accessorID == "" {
		return errEmptyID
	}
	// The API rejects is_owner for teams even when false, so it is only sent when true.
	perm := map[string]any{"id": accessorID, "access_type": accessType}
	if isOwner {
		perm["is_owner"] = true
	}
	body := map[string]any{"type": accessorType, "permissions": []map[string]any{perm}}
	return c.Do(ctx, http.MethodPost, folderPath(folderID)+"/permissions", body, nil)
}

// UpdateFolderPermission changes access_type and, when makeOwner is true, makes the accessor the owner
// (the API only allows setting is_owner to true).
func (c *Client) UpdateFolderPermission(ctx context.Context, folderID, accessorID, accessType string, makeOwner bool) error {
	if folderID == "" || accessorID == "" {
		return errEmptyID
	}
	body := map[string]any{"access_type": accessType}
	if makeOwner {
		body["is_owner"] = true
	}
	return c.Do(ctx, http.MethodPatch, permPath(folderID, accessorID), body, nil)
}

func (c *Client) RevokeFolderPermission(ctx context.Context, folderID, accessorID string) error {
	if folderID == "" || accessorID == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodDelete, permPath(folderID, accessorID), nil, nil)
}
