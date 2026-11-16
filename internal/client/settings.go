package client

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// SettingsSections are the sections of the settings export, in the order the API documents them.
var SettingsSections = []string{"labels", "custom_fields", "workflows", "templates", "apps", "webhooks", "routing_rules"}

// settingsCacheTTL is how long a fetched export is reused. A variable so tests can shorten it. It is short on purpose:
// the point is to let the resources of one plan or apply share a single download, not to hide changes made elsewhere.
var settingsCacheTTL = 5 * time.Second

// GetSettings exports the organisation's configuration and returns each section as raw JSON.
// custom_fields is an object keyed by field type; every other section is an array.
//
// The export is cached for settingsCacheTTL and cleared by every write this client makes, so a refresh of many
// webhooks, apps, routing rules and label groups downloads it once instead of once per resource.
func (c *Client) GetSettings(ctx context.Context) (map[string]json.RawMessage, error) {
	c.exportMu.Lock() // held while fetching, so parallel readers wait for the first download instead of repeating it
	defer c.exportMu.Unlock()
	if c.exportData != nil && time.Since(c.exportFetched) < settingsCacheTTL {
		return copySections(c.exportData), nil
	}
	var out struct {
		Resources map[string]json.RawMessage `json:"resources"`
	}
	if err := c.Do(ctx, http.MethodGet, "/settings", nil, &out); err != nil {
		return nil, err
	}
	if out.Resources == nil {
		out.Resources = map[string]json.RawMessage{}
	}
	c.exportData, c.exportFetched = out.Resources, time.Now()
	return copySections(out.Resources), nil
}

// invalidateSettings drops the cached export. Callers need not hold exportMu.
func (c *Client) invalidateSettings() {
	c.exportMu.Lock()
	c.exportData = nil
	c.exportMu.Unlock()
}

func copySections(in map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

// customFieldTypes are the keys the API requires in the custom_fields object, even when empty.
var customFieldTypes = []string{"currency", "number", "percentage", "checkbox", "dropdown", "multichoice",
	"multi_select_dropdown", "date", "image", "richtext", "string", "textarea", "video"}

// SettingsChangeset lists, per section, the names the import created or updated (or would, for a dry run).
type SettingsChangeset struct {
	Create map[string][]string `json:"create"`
	Update map[string][]string `json:"update"`
}

// ApplySettings sends a settings bundle. Sections left out of resources are sent empty, which the API
// accepts. With execute=false nothing changes and the changeset only reports what would. The API wants
// the bundle wrapped in a top-level "resources" key (the published spec does not show this).
//
// The import matches everything by name and never deletes: callers must expect merge-only behaviour.
func (c *Client) ApplySettings(ctx context.Context, resources map[string]any, execute bool) (*SettingsChangeset, error) {
	full := map[string]any{}
	for _, s := range SettingsSections {
		if s == "custom_fields" {
			cf := map[string]any{}
			for _, t := range customFieldTypes {
				cf[t] = []any{}
			}
			full[s] = cf
		} else {
			full[s] = []any{}
		}
	}
	for k, v := range resources {
		full[k] = v
	}
	path := "/settings"
	if execute {
		path += "?execute=true"
	}
	var out struct {
		Changeset SettingsChangeset `json:"changeset"`
	}
	err := c.Do(ctx, http.MethodPost, path, map[string]any{"resources": full}, &out)
	if execute {
		c.invalidateSettings() // even on error: a failed import may still have applied part of the bundle
	}
	if err != nil {
		return nil, err
	}
	return &out.Changeset, nil
}

// Label is one label of a label group.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// LabelGroupExport is a label group as the settings export shows it.
type LabelGroupExport struct {
	LabelType      string  `json:"label_type"`
	HasSingleValue bool    `json:"has_single_value"`
	Labels         []Label `json:"labels"`
}

// ExportLabelGroups reads the label groups (with colours) from the settings export.
func (c *Client) ExportLabelGroups(ctx context.Context) ([]LabelGroupExport, error) {
	res, err := c.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	var out []LabelGroupExport
	if raw, ok := res["labels"]; ok {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpsertLabelGroup creates or updates a label group by name. Labels already on the group are kept: the API
// only adds labels and overwrites those with the same name.
func (c *Client) UpsertLabelGroup(ctx context.Context, name string, hasSingleValue bool, labels []Label, execute bool) (*SettingsChangeset, error) {
	if labels == nil {
		labels = []Label{}
	}
	group := map[string]any{"label_type": name, "has_single_value": hasSingleValue, "labels": labels}
	return c.ApplySettings(ctx, map[string]any{"labels": []any{group}}, execute)
}
