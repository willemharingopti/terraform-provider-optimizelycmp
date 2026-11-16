package provider

// These tests run the real provider, through the real Terraform CLI, against internal/mockcmp: an in-memory fake of the
// CMP API that reproduces the quirks found by testing the real one. They need no account and no credentials, so they run
// in CI and on every `go test`. They are skipped when no Terraform CLI is installed.
//
// The live acceptance tests (acc_test.go) check the same flows against the real API. When the real API surprises the
// provider, teach internal/mockcmp the same behaviour and add a test here.

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/mockcmp"
)

var terraformOnce sync.Once

// mockEnv starts a mock CMP and returns it with the provider factories and provider block that point at it.
func mockEnv(t *testing.T) (*mockcmp.Server, map[string]func() (tfprotov6.ProviderServer, error), string) {
	t.Helper()
	path, err := exec.LookPath("terraform")
	if err != nil && os.Getenv("TF_ACC_TERRAFORM_PATH") == "" {
		t.Skip("no terraform CLI on the PATH")
	}
	terraformOnce.Do(func() {
		if os.Getenv("TF_ACC_TERRAFORM_PATH") == "" {
			_ = os.Setenv("TF_ACC_TERRAFORM_PATH", path)
		}
	})
	t.Parallel()
	srv := mockcmp.New(t)
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"optimizelycmp": providerserver.NewProtocol6WithError(newProvider("test", client.WithHTTPClient(srv.Client()))()),
	}
	block := fmt.Sprintf(`
provider "optimizelycmp" {
  base_url      = %q
  token_url     = %q
  client_id     = %q
  client_secret = %q
}
`, srv.URL, srv.URL+"/token", mockcmp.ClientID, mockcmp.ClientSecret)
	return srv, factories, block
}

func mockTest(t *testing.T, factories map[string]func() (tfprotov6.ProviderServer, error), checkDestroy resource.TestCheckFunc, steps ...resource.TestStep) {
	t.Helper()
	resource.UnitTest(t, resource.TestCase{ProtoV6ProviderFactories: factories, CheckDestroy: checkDestroy, Steps: steps})
}

// capture stores an attribute value for a later check.
type capture struct{ v string }

func (c *capture) set(v string) error { c.v = v; return nil }
func (c *capture) same(v string) error {
	if v != c.v {
		return fmt.Errorf("changed from %q to %q", c.v, v)
	}
	return nil
}
func (c *capture) different(v string) error {
	if v == c.v {
		return fmt.Errorf("%q did not change", v)
	}
	return nil
}

func TestMockFolderAndPermission(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	team := srv.TeamID("Mock Team")
	owner := srv.UserID("ada@example.invalid")
	folders := func(child string, nested bool, perms string) string {
		parent := ""
		if nested {
			parent = "parent_folder_id = optimizelycmp_folder.parent.id"
		}
		return prov + fmt.Sprintf(`
resource "optimizelycmp_folder" "parent" {
  name = "Brand"
}
resource "optimizelycmp_folder" "child" {
  name = %q
  %s
}
%s`, child, parent, perms)
	}
	teamPerm := func(access string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_folder_permission" "team" {
  folder_id     = optimizelycmp_folder.parent.id
  accessor_id   = %q
  accessor_type = "team"
  access_type   = %q
}`, team, access)
	}
	mockTest(t, factories, func(*terraform.State) error {
		if n := srv.FolderCount(); n != 0 {
			return fmt.Errorf("%d folders left behind", n)
		}
		return nil
	},
		resource.TestStep{
			Config: folders("Logos", true, teamPerm("view")),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("optimizelycmp_folder.child", "path", "/Brand/Logos"),
				resource.TestCheckResourceAttrPair("optimizelycmp_folder.child", "parent_folder_id", "optimizelycmp_folder.parent", "id"),
				resource.TestCheckResourceAttr("optimizelycmp_folder_permission.team", "is_owner", "false"),
			),
		},
		resource.TestStep{Config: folders("Icons", true, teamPerm("edit")), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("optimizelycmp_folder.child", "name", "Icons"),
			resource.TestCheckResourceAttr("optimizelycmp_folder_permission.team", "access_type", "edit"),
		)},
		resource.TestStep{ // removing parent_folder_id moves the folder to the root
			Config: folders("Icons", false, teamPerm("edit")),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckNoResourceAttr("optimizelycmp_folder.child", "parent_folder_id"),
				resource.TestCheckResourceAttr("optimizelycmp_folder.child", "path", "/Icons"),
			),
		},
		resource.TestStep{Config: folders("Icons", false, teamPerm("edit")), ResourceName: "optimizelycmp_folder.child", ImportState: true, ImportStateVerify: true},
		resource.TestStep{Config: folders("Icons", false, teamPerm("edit")), ResourceName: "optimizelycmp_folder_permission.team", ImportState: true, ImportStateVerify: true},
		resource.TestStep{ // a user can own a folder
			Config: folders("Icons", false, teamPerm("edit")+fmt.Sprintf(`
resource "optimizelycmp_folder_permission" "owner" {
  folder_id     = optimizelycmp_folder.parent.id
  accessor_id   = %q
  accessor_type = "user"
  access_type   = "edit"
  is_owner      = true
}`, owner)),
			Check: resource.TestCheckResourceAttr("optimizelycmp_folder_permission.owner", "is_owner", "true"),
		},
		resource.TestStep{ // ... a team cannot
			Config: folders("Icons", false, teamPerm("edit")+fmt.Sprintf(`
resource "optimizelycmp_folder_permission" "bad" {
  folder_id     = optimizelycmp_folder.parent.id
  accessor_id   = %q
  accessor_type = "team"
  access_type   = "edit"
  is_owner      = true
}`, srv.TeamID("Other Team"))),
			ExpectError: regexp.MustCompile(`(?i)team cannot be owner`),
		},
		resource.TestStep{Config: folders("Icons", false, teamPerm("edit"))}, // back to a valid config for the final destroy
	)
}

func TestMockContentType(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	const typ = "optimizelycmp_content_type.t"
	cfg := func(description string, withFlag bool) string {
		flag := ""
		if withFlag {
			flag = `, {
      core = { key = "featured", name = "Featured", field_type = "boolean", is_list = false, is_required = false, need_internationalization = false }
      base_type = "boolean"
    }`
		}
		return prov + fmt.Sprintf(`
data "optimizelycmp_user" "me" {
  email = "ada@example.invalid"
}
resource "optimizelycmp_content_type" "t" {
  name             = "Article"
  description      = %q
  component        = false
  acting_user_id   = data.optimizelycmp_user.me.id
  expected_locales = ["en"]
  field_definitions_json = jsonencode([{
      core = { key = "headline", name = "Headline", field_type = "text-field", is_list = false, is_required = true, need_internationalization = true }
      validation_pattern = ""
      min_length         = 1
    }%s])
}`, description, flag)
	}
	var id, ver capture
	mockTest(t, factories, func(*terraform.State) error {
		exists, disabled, _ := srv.ContentTypeState(id.v)
		if !exists || !disabled {
			return fmt.Errorf("destroy should leave the content type disabled (exists=%v disabled=%v)", exists, disabled)
		}
		return nil
	},
		resource.TestStep{ // the mock rewrites the empty validation_pattern; that must not show up as drift
			Config: cfg("first", false),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(typ, "id", id.set),
				resource.TestCheckResourceAttrWith(typ, "latest_version_id", ver.set),
			),
		},
		resource.TestStep{ // details only: no new version
			Config: cfg("second", false),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(typ, "description", "second"),
				resource.TestCheckResourceAttrWith(typ, "latest_version_id", ver.same),
			),
		},
		resource.TestStep{ // a new field publishes a new version
			Config: cfg("second", true),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrWith(typ, "latest_version_id", ver.different),
				func(*terraform.State) error {
					if _, _, n := srv.ContentTypeState(id.v); n != 2 {
						return fmt.Errorf("want 2 versions, mock has %d", n)
					}
					return nil
				},
			),
		},
		resource.TestStep{Config: cfg("second", true), ResourceName: typ, ImportState: true, ImportStateVerify: true,
			ImportStateVerifyIgnore: []string{"acting_user_id", "field_definitions_json", "expected_locales"}},
	)
}

func TestMockField(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	var ddID, numID, highID capture
	cfg := func(helper, choices string) string {
		return prov + fmt.Sprintf(`
resource "optimizelycmp_field" "dd" {
  name        = "Priority"
  type        = "dropdown"
  helper_text = %q
  choices = {
%s
  }
}
resource "optimizelycmp_field" "num" {
  name                   = "Budget"
  type                   = "currency_number"
  currency_code          = "EUR"
  has_thousand_separator = true
}`, helper, choices)
	}
	c1 := `    low  = { name = "Low" }
    high = { name = "High" }`
	c2 := `    low    = { name = "Low" }
    high   = { name = "Urgent" }
    medium = { name = "Medium" }`
	c3 := `    low  = { name = "Low", is_active = false }
    high = { name = "Urgent" }`
	mockTest(t, factories, func(*terraform.State) error {
		for _, id := range []string{ddID.v, numID.v} {
			if exists, active := srv.FieldState(id); !exists || active {
				return fmt.Errorf("destroy should deactivate field %s (exists=%v active=%v)", id, exists, active)
			}
		}
		return nil
	},
		resource.TestStep{Config: cfg("first", c1), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttrWith("optimizelycmp_field.dd", "id", ddID.set),
			resource.TestCheckResourceAttrWith("optimizelycmp_field.num", "id", numID.set),
			resource.TestCheckResourceAttrWith("optimizelycmp_field.dd", "choices.high.id", highID.set),
			resource.TestCheckResourceAttr("optimizelycmp_field.num", "decimal_places", "2"),
		)},
		resource.TestStep{ // rename keeps the ID, a new key adds a choice, and adding must not trip "inconsistent result"
			Config: cfg("second", c2),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.high.name", "Urgent"),
				resource.TestCheckResourceAttrWith("optimizelycmp_field.dd", "choices.high.id", highID.same),
				resource.TestCheckResourceAttrSet("optimizelycmp_field.dd", "choices.medium.id"),
			),
		},
		resource.TestStep{ // deactivate one, delete one
			Config: cfg("second", c3),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.%", "2"),
				resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.low.is_active", "false"),
			),
		},
		resource.TestStep{Config: cfg("second", c3), ResourceName: "optimizelycmp_field.num", ImportState: true, ImportStateVerify: true},
		resource.TestStep{Config: cfg("second", c3), ResourceName: "optimizelycmp_field.dd", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"choices"}},
	)
}

func TestMockLabelGroup(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	const g = "optimizelycmp_label_group.g"
	cfg := func(single bool, labels, extra string) string {
		return prov + fmt.Sprintf(`
resource "optimizelycmp_label_group" "g" {
  name             = "Pillars"
  has_single_value = %t
  labels = {
%s
  }
}
%s`, single, labels, extra)
	}
	l1 := `    Alpha = { color = "#4ECFD5" }
    Beta  = { color = "#FFC700" }`
	l2 := `    Alpha = { color = "#5F9FF6" }
    Beta  = { color = "#FFC700" }
    Gamma = { color = "#FF98A7" }`
	mockTest(t, factories, nil,
		resource.TestStep{Config: cfg(false, l1, ""), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(g, "labels.%", "2"),
			resource.TestCheckResourceAttr(g, "labels.Alpha.color", "#4ECFD5"),
		)},
		resource.TestStep{Config: cfg(false, l2, ""), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(g, "labels.%", "3"),
			resource.TestCheckResourceAttr(g, "labels.Alpha.color", "#5F9FF6"),
		)},
		resource.TestStep{ // a colour the API refuses fails at plan time
			Config:      cfg(false, `    Alpha = { color = "#702BD5" }`, ""),
			ExpectError: regexp.MustCompile(`(?s)color.*must be one of`),
		},
		resource.TestStep{ // the API cannot remove a label
			Config:      cfg(false, `    Alpha = { color = "#5F9FF6" }`, ""),
			ExpectError: regexp.MustCompile(`Labels cannot be removed`),
		},
		resource.TestStep{ // a second resource for an existing name fails instead of silently updating it
			Config: cfg(false, l2, `
resource "optimizelycmp_label_group" "dup" {
  name   = "Pillars"
  labels = { Delta = { color = "#4ECFD5" } }
}`),
			ExpectError: regexp.MustCompile(`already exists`),
		},
		resource.TestStep{Config: cfg(true, l2, ""), Check: resource.TestCheckResourceAttr(g, "has_single_value", "true")},
		resource.TestStep{Config: cfg(true, l2, ""), ResourceName: g, ImportState: true, ImportStateId: "Pillars", ImportStateVerify: true},
		resource.TestStep{
			Config: cfg(true, l2, ""),
			Check: func(*terraform.State) error {
				if n := srv.LabelGroups("Pillars"); n != 1 {
					return fmt.Errorf("want exactly 1 group named Pillars, mock has %d", n)
				}
				return nil
			},
		},
	)
}

// A group created in the CMP interface is known to the import by a slug ("content_pillar"), not its display name.
// Writing the display name would create a second group, so the provider must write the slug.
func TestMockLabelGroupCreatedInTheUI(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	cfg := func(labels, extra string) string {
		return prov + fmt.Sprintf(`
resource "optimizelycmp_label_group" "pillar" {
  name   = "Content Pillar"
  labels = {%s}
}
%s`, labels, extra)
	}
	mockTest(t, factories, nil,
		resource.TestStep{ // adopt the UI-created group
			Config: cfg("", ""), ResourceName: "optimizelycmp_label_group.pillar", ImportState: true, ImportStateId: "Content Pillar", ImportStatePersist: true,
		},
		resource.TestStep{
			Config: cfg("", ""),
			Check:  resource.TestCheckResourceAttr("optimizelycmp_label_group.pillar", "has_single_value", "false"),
		},
		resource.TestStep{ // adding a label must update that group, not create a second one
			Config: cfg(` Brand = { color = "#4ECFD5" } `, ""),
			Check: func(*terraform.State) error {
				if n := srv.LabelGroups("Content Pillar"); n != 1 {
					return fmt.Errorf("want 1 group, mock has %d: the display name was written and created a duplicate", n)
				}
				if got := srv.LabelNames("content_pillar"); len(got) != 1 || got[0] != "Brand" {
					return fmt.Errorf("the label did not reach the existing group: %v", got)
				}
				return nil
			},
		},
		resource.TestStep{ // creating a group whose name exists in either spelling is refused
			Config: cfg(` Brand = { color = "#4ECFD5" } `, `
resource "optimizelycmp_label_group" "again" {
  name   = "content_pillar"
  labels = {}
}`),
			ExpectError: regexp.MustCompile(`already exists`),
		},
		resource.TestStep{Config: cfg(` Brand = { color = "#4ECFD5" } `, "")},
	)
}

func TestMockWebhook(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	const w = "optimizelycmp_webhook.w"
	cfg := func(description, events, extra string) string {
		return prov + fmt.Sprintf(`
resource "optimizelycmp_webhook" "w" {
  name         = "tasks"
  description  = %q
  callback_url = "https://hooks.example.invalid/cmp"
  secret       = "s3cret"
  event_names  = [%s]
}
%s`, description, events, extra)
	}
	mockTest(t, factories, nil,
		resource.TestStep{Config: cfg("first", `"asset_added"`, ""), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(w, "id", "tasks"),
			resource.TestCheckResourceAttr(w, "secret", "s3cret"),
		)},
		resource.TestStep{Config: cfg("second", `"asset_added", "task_removed"`, ""), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(w, "description", "second"),
			resource.TestCheckResourceAttr(w, "event_names.#", "2"),
		)},
		resource.TestStep{Config: cfg("second", `"asset_added"`, ""), ExpectError: regexp.MustCompile(`Events cannot be removed`)},
		resource.TestStep{ // real events the import does not support are refused before anything changes
			Config: cfg("second", `"asset_added", "task_removed", "task_added"`, ""), ExpectError: regexp.MustCompile(`invalid events`),
		},
		resource.TestStep{Config: strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), "https://hooks.example.invalid", "http://hooks.example.invalid", 1), ExpectError: regexp.MustCompile(`(?s)must\s+be\s+an\s+https\s+URL`)},
		resource.TestStep{ // a duplicate name fails instead of updating the first
			Config: cfg("second", `"asset_added", "task_removed"`, `
resource "optimizelycmp_webhook" "dup" {
  name         = "tasks"
  callback_url = "https://hooks.example.invalid/other"
  secret       = "x"
  event_names  = ["asset_added"]
}`),
			ExpectError: regexp.MustCompile(`already exists`),
		},
		resource.TestStep{Config: strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), "s3cret", "rotated", 1), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(w, "secret", "rotated"),
			func(*terraform.State) error {
				if n := srv.HookCount("tasks"); n != 1 {
					return fmt.Errorf("want 1 webhook named tasks, mock has %d", n)
				}
				return nil
			},
		)},
		resource.TestStep{Config: strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), "s3cret", "rotated", 1), ResourceName: w, ImportState: true, ImportStateId: "tasks", ImportStateVerify: true},
	)
}

// If several webhooks share a name the import would create yet another one, so Terraform must refuse to touch them.
func TestMockWebhookAmbiguousName(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	srv.SeedHook("dup", "https://a.example.invalid", "s", "asset_added")
	srv.SeedHook("dup", "https://b.example.invalid", "s", "asset_added")
	mockTest(t, factories, nil,
		resource.TestStep{
			Config: prov + `
resource "optimizelycmp_webhook" "w" {
  name         = "dup"
  callback_url = "https://a.example.invalid"
  secret       = "s"
  event_names  = ["asset_added"]
}`,
			ResourceName: "optimizelycmp_webhook.w", ImportState: true, ImportStateId: "dup",
			ExpectError: regexp.MustCompile(`(?s)2 webhooks are named|cannot tell them apart`),
		},
	)
	if n := srv.HookCount("dup"); n != 2 {
		t.Errorf("the provider changed the number of webhooks named dup to %d", n)
	}
}

func TestMockApp(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	const a = "optimizelycmp_app.a"
	cfg := func(callbacks, extra string) string {
		return prov + fmt.Sprintf(`
resource "optimizelycmp_app" "a" {
  name                        = "portal"
  homepage_url                = "https://portal.example.invalid"
  authorization_callback_urls = [%s]
}
%s`, callbacks, extra)
	}
	both := `"https://portal.example.invalid/cb1", "https://portal.example.invalid/cb2"`
	mockTest(t, factories, nil,
		resource.TestStep{Config: cfg(`"https://portal.example.invalid/cb1"`, ""), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(a, "env", "production"),
			resource.TestCheckResourceAttr(a, "expose_email", "false"),
		)},
		resource.TestStep{Config: cfg(both, ""), Check: resource.TestCheckResourceAttr(a, "authorization_callback_urls.#", "2")},
		resource.TestStep{Config: cfg(`"https://portal.example.invalid/cb1"`, ""), ExpectError: regexp.MustCompile(`Callback URLs cannot be removed`)},
		resource.TestStep{Config: cfg(`"http://portal.example.invalid/cb1"`, ""), ExpectError: regexp.MustCompile(`(?s)must\s+be\s+an\s+https\s+URL`)},
		resource.TestStep{
			Config: cfg(both, `
resource "optimizelycmp_app" "dup" {
  name                        = "portal"
  homepage_url                = "https://x.example.invalid"
  authorization_callback_urls = ["https://x.example.invalid/cb"]
}`),
			ExpectError: regexp.MustCompile(`already exists`),
		},
		resource.TestStep{Config: cfg(both, ""), Check: func(*terraform.State) error {
			if n := srv.AppCount("portal"); n != 1 {
				return fmt.Errorf("want 1 app named portal, mock has %d", n)
			}
			return nil
		}},
		resource.TestStep{Config: cfg(both, ""), ResourceName: a, ImportState: true, ImportStateId: "portal", ImportStateVerify: true},
	)
}

func TestMockRoutingRule(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	const r = "optimizelycmp_routing_rule.r"
	cfg := func(description, template, watchers, rules string) string {
		tpl, w := "", ""
		if template != "" {
			tpl = fmt.Sprintf("template_name = %q", template)
		}
		if watchers != "" {
			w = fmt.Sprintf("watchers = [%s]", watchers)
		}
		return prov + fmt.Sprintf(`
resource "optimizelycmp_routing_rule" "r" {
  name        = "legal"
  description = %q
  %s
  %s
  %s
}`, description, tpl, w, rules)
	}
	label := `rules = [{
    field_type = "label"
    identifier = "content_pillar"
    operator   = "equal"
    value      = ["Brand"]
  }]`
	mockTest(t, factories, nil,
		resource.TestStep{Config: cfg("first", "", "", ""), Check: resource.ComposeAggregateTestCheckFunc(
			// CMP adds a default watcher on its own; it must not appear in state or show as a difference
			resource.TestCheckNoResourceAttr(r, "watchers.#"),
		)},
		resource.TestStep{Config: cfg("second", "Mock Template", "", ""), Check: resource.TestCheckResourceAttr(r, "template_name", "Mock Template")},
		resource.TestStep{ // CMP silently drops a template that does not exist: the apply must say so
			Config: cfg("second", "No Such Template", "", ""), ExpectError: regexp.MustCompile(`(?s)template_name.*was not kept`),
		},
		resource.TestStep{ // ... and a watcher that is not a CMP user
			Config: cfg("second", "Mock Template", `"nobody@example.invalid"`, ""), ExpectError: regexp.MustCompile(`(?s)watchers.*were not kept`),
		},
		resource.TestStep{Config: cfg("second", "Mock Template", `"ada@example.invalid"`, ""), Check: resource.TestCheckResourceAttr(r, "watchers.#", "1")},
		resource.TestStep{ // watchers cannot be removed
			Config: cfg("second", "Mock Template", "", ""), ExpectError: regexp.MustCompile(`Watchers cannot be removed`),
		},
		resource.TestStep{Config: cfg("second", "Mock Template", `"ada@example.invalid"`, label), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(r, "rules.#", "1"),
			resource.TestCheckResourceAttr(r, "rules.0.identifier", "content_pillar"),
		)},
		resource.TestStep{ // a due date condition is refused by the API; its message comes through
			Config: cfg("second", "Mock Template", `"ada@example.invalid"`, `rules = [{
    field_type = "dueDate"
    operator   = "equal"
    value      = ["3"]
    unit       = "days"
  }]`),
			ExpectError: regexp.MustCompile(`(?s)due date`),
		},
		resource.TestStep{Config: cfg("second", "", `"ada@example.invalid"`, label), Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckNoResourceAttr(r, "template_name"),
			func(*terraform.State) error {
				if n := srv.RuleCount("legal"); n != 1 {
					return fmt.Errorf("want 1 rule named legal, mock has %d", n)
				}
				return nil
			},
		)},
		resource.TestStep{
			Config: cfg("second", "", `"ada@example.invalid"`, label) + `
resource "optimizelycmp_routing_rule" "dup" {
  name = "legal"
}`,
			ExpectError: regexp.MustCompile(`already exists`),
		},
		resource.TestStep{Config: cfg("second", "", `"ada@example.invalid"`, label), ResourceName: r, ImportState: true, ImportStateId: "legal", ImportStateVerify: true,
			ImportStateVerifyIgnore: []string{"watchers"}}, // an import does not adopt watchers: CMP adds one of its own
	)
}

func TestMockDataSources(t *testing.T) {
	srv, factories, prov := mockEnv(t)
	team := srv.TeamID("Mock Team")
	user := srv.UserID("ada@example.invalid")
	mockTest(t, factories, nil, resource.TestStep{
		Config: prov + `
data "optimizelycmp_user" "u" {
  email = "ada@example.invalid"
}
data "optimizelycmp_users" "all" {}
data "optimizelycmp_team" "t" {
  name = "mock team"
}
data "optimizelycmp_workflow" "w" {
  name = "Mock Workflow"
}
data "optimizelycmp_template" "p" {
  title = "Mock Template"
}
data "optimizelycmp_publishing_channel" "c" {
  name = "Web"
}
data "optimizelycmp_label_group" "g" {
  name = "Content Pillar"
}
data "optimizelycmp_brand_compliance_category" "b" {
  name = "Tone"
}
data "optimizelycmp_settings" "s" {}
`,
		Check: resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr("data.optimizelycmp_user.u", "id", user), // found through the 302 redirect
			resource.TestCheckResourceAttr("data.optimizelycmp_user.u", "full_name", "Ada Lovelace"),
			resource.TestCheckResourceAttr("data.optimizelycmp_users.all", "users.#", "2"),
			resource.TestCheckResourceAttr("data.optimizelycmp_team.t", "id", team),
			resource.TestCheckResourceAttr("data.optimizelycmp_team.t", "user_emails.0", "ada@example.invalid"),
			resource.TestCheckResourceAttr("data.optimizelycmp_workflow.w", "step_names.#", "2"),
			resource.TestCheckResourceAttr("data.optimizelycmp_workflow.w", "is_smart_duration_enabled", "true"),
			resource.TestCheckResourceAttr("data.optimizelycmp_template.p", "applicable_to.0", "work_request"),
			resource.TestCheckResourceAttr("data.optimizelycmp_publishing_channel.c", "disabled", "false"),
			resource.TestCheckResourceAttr("data.optimizelycmp_label_group.g", "source_org_type", "current"),
			resource.TestCheckResourceAttr("data.optimizelycmp_brand_compliance_category.b", "criteria_names.0", "Friendly"),
			resource.TestCheckResourceAttr("data.optimizelycmp_settings.s", "id", "settings"),
			resource.TestCheckResourceAttrWith("data.optimizelycmp_settings.s", "labels_json", func(v string) error {
				if !strings.Contains(v, "content_pillar") {
					return fmt.Errorf("export labels do not mention the UI-created group's slug: %s", v)
				}
				return nil
			}),
		),
	},
		resource.TestStep{ // a lookup that finds nothing fails clearly
			Config:      prov + "data \"optimizelycmp_team\" \"missing\" {\n  name = \"no such team\"\n}\n",
			ExpectError: regexp.MustCompile(`nothing found matching`),
		},
	)
}

// The provider must reject credentials it cannot use instead of failing later with a confusing error.
func TestMockBadCredentials(t *testing.T) {
	srv, factories, _ := mockEnv(t)
	mockTest(t, factories, nil, resource.TestStep{
		Config: fmt.Sprintf(`
provider "optimizelycmp" {
  base_url      = %q
  token_url     = %q
  client_id     = "wrong"
  client_secret = "wrong"
}
data "optimizelycmp_team" "t" {
  name = "Mock Team"
}
`, srv.URL, srv.URL+"/token"),
		ExpectError: regexp.MustCompile(`token request failed with status 401`),
	})
}
