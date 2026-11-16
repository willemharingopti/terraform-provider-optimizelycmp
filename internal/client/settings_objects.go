package client

import (
	"context"
	"encoding/json"
)

// Webhook is a webhook as the settings export shows it. The export returns the secret in full.
type Webhook struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CallbackURL string   `json:"callback_url"`
	Secret      string   `json:"secret"`
	EventNames  []string `json:"event_names"`
}

// App is an app (OAuth client registration) as the settings export shows it.
type App struct {
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	HomepageURL              string   `json:"homepage_url"`
	AuthorizationCallbackURL []string `json:"authorization_callback_urls"`
	ExposeEmail              bool     `json:"expose_email"`
	Env                      string   `json:"env"`
}

// RoutingCondition is one condition of a routing rule.
type RoutingCondition struct {
	FieldType       string   `json:"field_type"`
	Identifier      *string  `json:"identifier"`
	Operator        string   `json:"operator"`
	Value           []string `json:"value"`
	Unit            *string  `json:"unit"`
	CustomFieldType *string  `json:"custom_field_type"`
}

// RoutingRule is a routing rule as the settings export shows it.
type RoutingRule struct {
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	TemplateName *string            `json:"template_name"`
	Watchers     []string           `json:"watchers"`
	Rules        []RoutingCondition `json:"rules"`
}

func exportSection[T any](ctx context.Context, c *Client, section string) ([]T, error) {
	res, err := c.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	var out []T
	if raw, ok := res[section]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *Client) ExportWebhooks(ctx context.Context) ([]Webhook, error) {
	return exportSection[Webhook](ctx, c, "webhooks")
}

func (c *Client) ExportApps(ctx context.Context) ([]App, error) {
	return exportSection[App](ctx, c, "apps")
}

func (c *Client) ExportRoutingRules(ctx context.Context) ([]RoutingRule, error) {
	return exportSection[RoutingRule](ctx, c, "routing_rules")
}

// SettingsNames lists the names in one section of the export (apps, webhooks, routing_rules, ...).
func (c *Client) SettingsNames(ctx context.Context, section string) ([]string, error) {
	items, err := exportSection[struct {
		Name string `json:"name"`
	}](ctx, c, section)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	return names, nil
}

// ApplySettingsItem writes one item of one section (the others are sent empty). See ApplySettings.
func (c *Client) ApplySettingsItem(ctx context.Context, section string, item any, execute bool) (*SettingsChangeset, error) {
	return c.ApplySettings(ctx, map[string]any{section: []any{item}}, execute)
}
