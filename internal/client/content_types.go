package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// ContentTypeVersion is a version of a structured content type.
type ContentTypeVersion struct {
	VersionGUID     string          `json:"version_guid"`
	Latest          bool            `json:"latest"`
	ExpectedLocales []string        `json:"expected_locales"`
	FieldDefs       json.RawMessage `json:"field_definitions"`
}

// ContentType is a structured content type with its latest version.
type ContentType struct {
	GUID          string              `json:"content_type_guid"`
	Name          string              `json:"name"`
	Description   *string             `json:"description"`
	Component     bool                `json:"component"`
	Disabled      bool                `json:"disabled"`
	ThumbnailGUID *string             `json:"thumbnail_guid"`
	CreatedAt     string              `json:"created_at"`
	UpdatedAt     string              `json:"updated_at"`
	LatestVersion *ContentTypeVersion `json:"latest_version"`
}

// ContentTypeDetails are the editable properties of a content type.
type ContentTypeDetails struct {
	Name          string  `json:"name"`
	Description   *string `json:"description,omitempty"`
	Component     bool    `json:"component"`
	Disabled      bool    `json:"disabled"`
	ThumbnailGUID *string `json:"thumbnail_guid,omitempty"`
}

const ctPath = "/structured-content/content-types"

func ctItem(id string) string { return ctPath + "/" + url.PathEscape(id) }

// CreateContentType creates a content type with its first version and returns its GUID.
func (c *Client) CreateContentType(ctx context.Context, d ContentTypeDetails, fieldDefs json.RawMessage, locales []string, createdBy string) (string, error) {
	body := map[string]any{"details": d, "field_definitions": fieldDefs, "created_by": createdBy}
	if len(locales) > 0 {
		body["expected_locales"] = locales
	}
	var out struct {
		Created bool   `json:"created"`
		GUID    string `json:"content_type_guid"`
	}
	if err := c.Do(ctx, http.MethodPost, ctPath, body, &out); err != nil {
		return "", err
	}
	if out.GUID == "" {
		return "", errors.New("create content type: the API returned no content_type_guid")
	}
	return out.GUID, nil
}

func (c *Client) GetContentType(ctx context.Context, id string) (*ContentType, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out ContentType
	if err := c.Do(ctx, http.MethodGet, ctItem(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateContentType changes the details (not the fields: those need a new version).
func (c *Client) UpdateContentType(ctx context.Context, id string, d ContentTypeDetails, updatedBy string) error {
	if id == "" {
		return errEmptyID
	}
	body := map[string]any{"details": d, "updated_by": updatedBy}
	var out struct {
		Updated bool `json:"updated"`
	}
	if err := c.Do(ctx, http.MethodPost, ctItem(id), body, &out); err != nil {
		return err
	}
	if !out.Updated {
		return errors.New("update content type: the API reported updated=false")
	}
	return nil
}

// AddContentTypeVersion publishes a new version with the given fields and locales.
func (c *Client) AddContentTypeVersion(ctx context.Context, id string, fieldDefs json.RawMessage, locales []string, createdBy string) error {
	if id == "" {
		return errEmptyID
	}
	body := map[string]any{"field_definitions": fieldDefs, "created_by": createdBy}
	if len(locales) > 0 {
		body["expected_locales"] = locales
	}
	return c.Do(ctx, http.MethodPost, ctItem(id)+"/versions", body, nil)
}

// ListContentTypes returns every content type (components and content, enabled and disabled).
func (c *Client) ListContentTypes(ctx context.Context) ([]ContentType, error) {
	var out []ContentType
	if err := c.Do(ctx, http.MethodGet, ctPath+"?list=3", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
