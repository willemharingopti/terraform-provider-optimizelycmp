package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type User struct {
	ID        string  `json:"id"`
	FirstName string  `json:"first_name"`
	LastName  string  `json:"last_name"`
	FullName  string  `json:"full_name"`
	Email     *string `json:"email"`
}

type Team struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Workflow struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IsFlexible  bool    `json:"is_flexible"`
}

type Template struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	IsActive bool   `json:"is_active"`
}

type PublishingChannel struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
}

// FindUserByEmail resolves the 302 that GET /users?email= answers with, then reads the user.
func (c *Client) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	h, err := c.do(ctx, http.MethodGet, "/users?email="+url.QueryEscape(email), nil, nil, true)
	if err != nil {
		return nil, err
	}
	loc := h.Get("Location")
	if loc == "" {
		return nil, ErrNotFound
	}
	id := loc[strings.LastIndex(loc, "/")+1:]
	if i := strings.IndexAny(id, "?#"); i >= 0 {
		id = id[:i]
	}
	if id == "" {
		return nil, fmt.Errorf("unexpected user redirect %q", loc)
	}
	var u User
	if err := c.Do(ctx, http.MethodGet, "/users/"+url.PathEscape(id), nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

func (c *Client) ListTeams(ctx context.Context) ([]Team, error) {
	return listAll[Team](ctx, c, "/teams")
}

func (c *Client) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	return listAll[Workflow](ctx, c, "/workflows")
}

// ListTemplates searches templates by title text; callers pick the exact match.
func (c *Client) ListTemplates(ctx context.Context, search string) ([]Template, error) {
	path := "/templates?include_inactive=true"
	if search != "" {
		path += "&search=" + url.QueryEscape(search)
	}
	return listAll[Template](ctx, c, path)
}

func (c *Client) ListPublishingChannels(ctx context.Context) ([]PublishingChannel, error) {
	return listAll[PublishingChannel](ctx, c, "/publishing-channels")
}

// TeamDetail is a team with its members.
type TeamDetail struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Users []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"users"`
}

func (c *Client) GetTeam(ctx context.Context, id string) (*TeamDetail, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out TeamDetail
	if err := c.Do(ctx, http.MethodGet, "/teams/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WorkflowDetail is a workflow with its flags and step names.
type WorkflowDetail struct {
	ID                          string  `json:"id"`
	Name                        string  `json:"name"`
	Description                 *string `json:"description"`
	IsActive                    bool    `json:"is_active"`
	IsFlexible                  bool    `json:"is_flexible"`
	IsSmartDurationEnabled      bool    `json:"is_smart_duration_enabled"`
	IsAssetApprovalEnabled      bool    `json:"is_asset_approval_enabled"`
	IsResourceManagementEnabled bool    `json:"is_resource_management_enabled"`
	Steps                       []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"steps"`
}

func (c *Client) GetWorkflow(ctx context.Context, id string) (*WorkflowDetail, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out WorkflowDetail
	if err := c.Do(ctx, http.MethodGet, "/workflows/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TemplateDetail is a template without its form fields.
type TemplateDetail struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	IsActive     bool     `json:"is_active"`
	ApplicableTo []string `json:"applicable_to"`
}

func (c *Client) GetTemplate(ctx context.Context, id string) (*TemplateDetail, error) {
	if id == "" {
		return nil, errEmptyID
	}
	var out TemplateDetail
	if err := c.Do(ctx, http.MethodGet, "/templates/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UserSummary is an entry of the organisation's user list.
type UserSummary struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	FullName  string `json:"full_name"`
	Roles     []struct {
		Name string `json:"name"`
	} `json:"roles"`
}

func (c *Client) ListUsers(ctx context.Context) ([]UserSummary, error) {
	return listAll[UserSummary](ctx, c, "/userlist")
}

type LabelGroup struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SourceOrgType string `json:"source_org_type"`
	Values        []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"values"`
}

func (c *Client) ListLabelGroups(ctx context.Context) ([]LabelGroup, error) {
	return listAll[LabelGroup](ctx, c, "/label-groups")
}

type BrandComplianceCategory struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Criteria []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"criteria"`
}

func (c *Client) ListBrandComplianceCategories(ctx context.Context) ([]BrandComplianceCategory, error) {
	return listAll[BrandComplianceCategory](ctx, c, "/brand-compliance/categories")
}
