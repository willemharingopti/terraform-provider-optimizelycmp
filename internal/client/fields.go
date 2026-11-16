package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// FieldChoice is one option of a choice-type custom field.
type FieldChoice struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	IsActive bool    `json:"is_active"`
	Color    *string `json:"color"`
}

// Field is an organisation-level custom field. The API returns different extra properties per type, so
// the type-specific ones are optional.
type Field struct {
	ID                   string        `json:"id"`
	Name                 string        `json:"name"`
	HelperText           *string       `json:"helper_text"`
	IsActive             bool          `json:"is_active"`
	Type                 string        `json:"type"`
	IsMultiSelect        *bool         `json:"is_multi_select"`
	Choices              []FieldChoice `json:"choices"`
	HasThousandSeparator *bool         `json:"has_thousand_separator"`
	DecimalPlaces        *int          `json:"decimal_places"`
	CurrencyCode         *string       `json:"currency_code"`
}

func fieldPath(id string) string { return "/fields/" + url.PathEscape(id) }

// CreateField creates a field from a type-specific payload and returns its ID.
func (c *Client) CreateField(ctx context.Context, payload map[string]any) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(ctx, http.MethodPost, "/fields", payload, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("create field: the API returned no id")
	}
	return out.ID, nil
}

// GetField reads one field. The API has no single-field GET, so it filters the list by ID.
func (c *Client) GetField(ctx context.Context, id string) (*Field, error) {
	if id == "" {
		return nil, errEmptyID
	}
	all, err := listAll[Field](ctx, c, "/fields?ids="+url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}
	return nil, ErrNotFound
}

func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	return listAll[Field](ctx, c, "/fields")
}

// UpdateField patches a field. payload must include "type" (required by the API, never changed).
func (c *Client) UpdateField(ctx context.Context, id string, payload map[string]any) error {
	if id == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodPatch, fieldPath(id), payload, nil)
}

// AddFieldChoices adds choices to a field.
func (c *Client) AddFieldChoices(ctx context.Context, fieldID string, choices []map[string]any) error {
	if fieldID == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodPost, fieldPath(fieldID)+"/choices", choices, nil)
}

func choicePath(fieldID, choiceID string) string {
	return fieldPath(fieldID) + "/choices/" + url.PathEscape(choiceID)
}

func (c *Client) UpdateFieldChoice(ctx context.Context, fieldID, choiceID string, payload map[string]any) error {
	if fieldID == "" || choiceID == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodPatch, choicePath(fieldID, choiceID), payload, nil)
}

// DeleteFieldChoice removes a choice. The API refuses to delete a field's last active choice.
func (c *Client) DeleteFieldChoice(ctx context.Context, fieldID, choiceID string) error {
	if fieldID == "" || choiceID == "" {
		return errEmptyID
	}
	return c.Do(ctx, http.MethodDelete, choicePath(fieldID, choiceID), nil, nil)
}
