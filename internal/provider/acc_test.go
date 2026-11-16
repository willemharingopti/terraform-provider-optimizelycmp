package provider

// Acceptance tests run against a REAL Optimizely CMP account and create, change and delete objects there.
// They are skipped unless TF_ACC=1 and need Terraform on the PATH (or TF_ACC_TERRAFORM_PATH).
//
//	make testacc          # runs the tests that leave nothing behind
//	make testacc-residue  # also runs the ones that leave deactivated objects behind
//
// or by hand:
//
//	TF_ACC=1 TF_ACC_TERRAFORM_PATH=$(which terraform) \
//	OPTIMIZELY_CMP_CLIENT_ID=... OPTIMIZELY_CMP_CLIENT_SECRET=... \
//	go test ./internal/provider -run TestAcc -v -timeout 30m
//
// Use a sandbox account. Everything is named "tfacc-<random>" so reruns never collide.
//
// The CMP API has no delete for content types and custom fields, so those tests leave a disabled content type or an
// inactive field behind on every run. They only run when CMP_ACC_ALLOW_RESIDUE=1, and someone with admin access has to
// remove the leftovers by hand. The folder, permission and data source tests clean up after themselves.
//
// The tests find a user and a team to use by listing the account, and skip if there is none.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

var testAccProviders = map[string]func() (tfprotov6.ProviderServer, error){
	"optimizelycmp": providerserver.NewProtocol6WithError(New("test")()),
}

// accGate skips unless acceptance tests are enabled, and fails if the credentials are missing.
func accGate(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests need TF_ACC=1")
	}
	for _, v := range []string{"OPTIMIZELY_CMP_CLIENT_ID", "OPTIMIZELY_CMP_CLIENT_SECRET"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

// accResidue is accGate for tests that cannot clean up after themselves.
func accResidue(t *testing.T) {
	t.Helper()
	accGate(t)
	if os.Getenv("CMP_ACC_ALLOW_RESIDUE") != "1" {
		t.Skip("this test leaves a deactivated object behind in CMP; set CMP_ACC_ALLOW_RESIDUE=1 to run it")
	}
}

func accClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(os.Getenv("OPTIMIZELY_CMP_BASE_URL"), os.Getenv("OPTIMIZELY_CMP_TOKEN_URL"),
		os.Getenv("OPTIMIZELY_CMP_CLIENT_ID"), os.Getenv("OPTIMIZELY_CMP_CLIENT_SECRET"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func accName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "tfacc-" + prefix + "-" + hex.EncodeToString(b)
}

func accFirstUserID(t *testing.T, c *client.Client) string {
	t.Helper()
	us, err := c.ListUsers(context.Background())
	if err != nil || len(us) == 0 {
		t.Skipf("no users to use (%v)", err)
	}
	return us[0].ID
}

func accFirstTeam(t *testing.T, c *client.Client) client.Team {
	t.Helper()
	ts, err := c.ListTeams(context.Background())
	if err != nil || len(ts) == 0 {
		t.Skipf("no teams to use (%v)", err)
	}
	return ts[0]
}

// goneFromAPI asserts every resource of tfType is gone from the API.
func goneFromAPI(tfType string, get func(ctx context.Context, c *client.Client, id string) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		c, err := client.New(os.Getenv("OPTIMIZELY_CMP_BASE_URL"), os.Getenv("OPTIMIZELY_CMP_TOKEN_URL"),
			os.Getenv("OPTIMIZELY_CMP_CLIENT_ID"), os.Getenv("OPTIMIZELY_CMP_CLIENT_SECRET"))
		if err != nil {
			return err
		}
		for _, rs := range s.RootModule().Resources {
			if rs.Type != tfType {
				continue
			}
			err := get(context.Background(), c, rs.Primary.ID)
			if err == nil {
				return fmt.Errorf("%s %q still exists", tfType, rs.Primary.ID)
			}
			if !errors.Is(err, client.ErrNotFound) {
				return err
			}
		}
		return nil
	}
}

func folderGone(tfType string) resource.TestCheckFunc {
	return goneFromAPI(tfType, func(ctx context.Context, c *client.Client, id string) error {
		_, err := c.GetFolder(ctx, id)
		return err
	})
}

// versionTracker remembers a version ID in one step and compares it in a later one.
type versionTracker struct{ v string }

func (vt *versionTracker) remember(v string) error { vt.v = v; return nil }
func (vt *versionTracker) same(v string) error {
	if v != vt.v {
		return fmt.Errorf("version changed from %s to %s, but no new version was expected", vt.v, v)
	}
	return nil
}
func (vt *versionTracker) different(v string) error {
	if v == vt.v {
		return fmt.Errorf("version %s is unchanged, but a new version was expected", v)
	}
	return nil
}

func TestAccFolder(t *testing.T) {
	accGate(t)
	parent, child := accName("parent"), accName("child")
	renamed := child + "-renamed"
	cfg := func(childName string, nested bool) string {
		parentRef := ""
		if nested {
			parentRef = "parent_folder_id = optimizelycmp_folder.parent.id"
		}
		return fmt.Sprintf(`
resource "optimizelycmp_folder" "parent" {
  name = %q
}

resource "optimizelycmp_folder" "child" {
  name = %q
  %s
}
`, parent, childName, parentRef)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             folderGone("optimizelycmp_folder"),
		Steps: []resource.TestStep{
			{
				Config: cfg(child, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_folder.child", "name", child),
					resource.TestCheckResourceAttrPair("optimizelycmp_folder.child", "parent_folder_id", "optimizelycmp_folder.parent", "id"),
					resource.TestCheckResourceAttr("optimizelycmp_folder.child", "path", "/"+parent+"/"+child),
					resource.TestCheckResourceAttrSet("optimizelycmp_folder.child", "created_at"),
				),
			},
			{Config: cfg(renamed, true), Check: resource.TestCheckResourceAttr("optimizelycmp_folder.child", "name", renamed)},
			{ // removing parent_folder_id moves the folder to the root
				Config: cfg(renamed, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("optimizelycmp_folder.child", "parent_folder_id"),
					resource.TestCheckResourceAttr("optimizelycmp_folder.child", "path", "/"+renamed),
				),
			},
			{Config: cfg(renamed, false), ResourceName: "optimizelycmp_folder.child", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestAccFolderPermission(t *testing.T) {
	accGate(t)
	team := accFirstTeam(t, accClient(t))
	folder := accName("perm")
	cfg := func(access string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_folder" "f" {
  name = %q
}

resource "optimizelycmp_folder_permission" "p" {
  folder_id     = optimizelycmp_folder.f.id
  accessor_id   = %q
  accessor_type = "team"
  access_type   = %q
}
`, folder, team.ID, access)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy:             folderGone("optimizelycmp_folder"),
		Steps: []resource.TestStep{
			{
				Config: cfg("view"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_folder_permission.p", "access_type", "view"),
					resource.TestCheckResourceAttr("optimizelycmp_folder_permission.p", "is_owner", "false"),
				),
			},
			{Config: cfg("edit"), Check: resource.TestCheckResourceAttr("optimizelycmp_folder_permission.p", "access_type", "edit")},
			{Config: cfg("edit"), ResourceName: "optimizelycmp_folder_permission.p", ImportState: true, ImportStateVerify: true},
			{ // a team cannot own a folder
				Config:      cfg("edit") + "\nresource \"optimizelycmp_folder_permission\" \"owner\" {\n  folder_id = optimizelycmp_folder.f.id\n  accessor_id = \"" + team.ID + "x\"\n  accessor_type = \"team\"\n  access_type = \"edit\"\n  is_owner = true\n}\n",
				ExpectError: regexp.MustCompile(`(?i)team cannot be owner`),
			},
		},
	})
}

func TestAccDataSources(t *testing.T) {
	accGate(t)
	c := accClient(t)
	team := accFirstTeam(t, c)
	ws, err := c.ListWorkflows(context.Background())
	if err != nil || len(ws) == 0 {
		t.Skipf("no workflows to use (%v)", err)
	}
	cfg := fmt.Sprintf(`
data "optimizelycmp_users" "all" {}

data "optimizelycmp_team" "t" {
  name = %q
}

data "optimizelycmp_workflow" "w" {
  name = %q
}
`, team.Name, ws[0].Name)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.optimizelycmp_users.all", "users.#"),
					resource.TestCheckResourceAttr("data.optimizelycmp_team.t", "id", team.ID),
					resource.TestCheckResourceAttr("data.optimizelycmp_workflow.w", "id", ws[0].ID),
					resource.TestCheckResourceAttrSet("data.optimizelycmp_workflow.w", "is_active"),
					resource.TestCheckResourceAttrSet("data.optimizelycmp_workflow.w", "step_names.#"),
				),
			},
			{
				Config:      `data "optimizelycmp_team" "missing" {` + "\n  name = \"tfacc-no-such-team\"\n}\n",
				ExpectError: regexp.MustCompile(`nothing found matching`),
			},
		},
	})
}

func TestAccSettingsDataSource(t *testing.T) {
	accGate(t)
	validJSON := func(wantFirst byte) resource.CheckResourceAttrWithFunc {
		return func(v string) error {
			if !json.Valid([]byte(v)) || len(v) == 0 || v[0] != wantFirst {
				return fmt.Errorf("not the expected JSON shape: %.60s", v)
			}
			return nil
		}
	}
	const addr = "data.optimizelycmp_settings.s"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{{
			Config: `data "optimizelycmp_settings" "s" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(addr, "id", "settings"),
				resource.TestCheckResourceAttrWith(addr, "labels_json", validJSON('[')),
				resource.TestCheckResourceAttrWith(addr, "custom_fields_json", validJSON('{')),
				resource.TestCheckResourceAttrWith(addr, "workflows_json", validJSON('[')),
				resource.TestCheckResourceAttrWith(addr, "templates_json", validJSON('[')),
				resource.TestCheckResourceAttrWith(addr, "apps_json", validJSON('[')),
				resource.TestCheckResourceAttrWith(addr, "webhooks_json", validJSON('[')),
				resource.TestCheckResourceAttrWith(addr, "routing_rules_json", validJSON('[')),
			),
		}},
	})
}

func TestAccLabelGroup(t *testing.T) {
	accResidue(t) // the API cannot delete label groups: destroy only forgets this one
	name := accName("labels")
	cfg := func(single bool, labels string, extra string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_label_group" "g" {
  name             = %q
  has_single_value = %t
  labels = {
%s
  }
}
%s`, name, single, labels, extra)
	}
	l1 := `    Alpha = { color = "#4ECFD5" }
    Beta  = { color = "#FFC700" }`
	l2 := `    Alpha = { color = "#5F9FF6" }
    Beta  = { color = "#FFC700" }
    Gamma = { color = "#FF98A7" }`
	const g = "optimizelycmp_label_group.g"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: cfg(false, l1, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(g, "id"),
					resource.TestCheckResourceAttr(g, "labels.%", "2"),
					resource.TestCheckResourceAttr(g, "labels.Alpha.color", "#4ECFD5"),
					resource.TestCheckResourceAttr(g, "has_single_value", "false"),
				),
			},
			{ // add a label and overwrite a colour
				Config: cfg(false, l2, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(g, "labels.%", "3"),
					resource.TestCheckResourceAttr(g, "labels.Alpha.color", "#5F9FF6"),
					resource.TestCheckResourceAttr(g, "labels.Gamma.color", "#FF98A7"),
				),
			},
			{ // a colour the API refuses must fail at plan time
				Config:      cfg(false, `    Alpha = { color = "#702BD5" }`, ""),
				ExpectError: regexp.MustCompile(`(?s)color.*must be one of`),
			},
			{ // the API cannot remove a label, so the plan must refuse
				Config:      cfg(false, `    Alpha = { color = "#5F9FF6" }`, ""),
				ExpectError: regexp.MustCompile(`Labels cannot be removed`),
			},
			{ // creating a second resource for an existing name must fail, not silently update it
				Config: cfg(false, l2, fmt.Sprintf(`
resource "optimizelycmp_label_group" "dup" {
  name   = %q
  labels = { Delta = { color = "#4ECFD5" } }
}
`, name)),
				ExpectError: regexp.MustCompile(`already exists`),
			},
			{ // switching to single-value
				Config: cfg(true, l2, ""),
				Check:  resource.TestCheckResourceAttr(g, "has_single_value", "true"),
			},
			{Config: cfg(true, l2, ""), ResourceName: g, ImportState: true, ImportStateId: name, ImportStateVerify: true},
		},
	})
}

func TestAccWebhook(t *testing.T) {
	accResidue(t) // the API cannot delete webhooks: destroy only forgets this one
	name, secret := accName("hook"), accName("secret")
	cfg := func(description, events, extra string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_webhook" "w" {
  name         = %q
  description  = %q
  callback_url = "https://example.invalid/tfacc"
  secret       = %q
  event_names  = [%s]
}
%s`, name, description, secret, events, extra)
	}
	const w = "optimizelycmp_webhook.w"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: cfg("first", `"asset_added"`, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(w, "id", name),
					resource.TestCheckResourceAttr(w, "event_names.#", "1"),
					resource.TestCheckResourceAttr(w, "secret", secret),
				),
			},
			{ // change a field and add an event
				Config: cfg("second", `"asset_added", "task_removed"`, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(w, "description", "second"),
					resource.TestCheckResourceAttr(w, "event_names.#", "2"),
				),
			},
			{ // events cannot be removed, so the plan must refuse
				Config:      cfg("second", `"asset_added"`, ""),
				ExpectError: regexp.MustCompile(`Events cannot be removed`),
			},
			{ // events the import does not support (even real ones like task_added) are refused by the API, before anything is changed
				Config:      cfg("second", `"asset_added", "task_removed", "task_added"`, ""),
				ExpectError: regexp.MustCompile(`invalid events`),
			},
			{ // a second resource with the same name must fail, not silently update the first
				Config: cfg("second", `"asset_added", "task_removed"`, fmt.Sprintf(`
resource "optimizelycmp_webhook" "dup" {
  name         = %q
  callback_url = "https://example.invalid/other"
  secret       = "tfacc-other"
  event_names  = ["asset_added"]
}
`, name)),
				ExpectError: regexp.MustCompile(`already exists`),
			},
			{ // http is refused at plan time
				Config:      strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), "https://", "http://", 1),
				ExpectError: regexp.MustCompile(`must be an https URL`),
			},
			{ // changing the secret
				Config: strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), secret, secret+"-rotated", 1),
				Check:  resource.TestCheckResourceAttr(w, "secret", secret+"-rotated"),
			},
			{
				Config:       strings.Replace(cfg("second", `"asset_added", "task_removed"`, ""), secret, secret+"-rotated", 1),
				ResourceName: w, ImportState: true, ImportStateId: name, ImportStateVerify: true,
			},
		},
	})
}

func TestAccApp(t *testing.T) {
	accResidue(t) // the API cannot delete apps: destroy only forgets this one
	name := accName("app")
	cfg := func(callbacks, extra string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_app" "a" {
  name                        = %q
  description                 = "created by an acceptance test"
  homepage_url                = "https://example.invalid"
  authorization_callback_urls = [%s]
}
%s`, name, callbacks, extra)
	}
	const a = "optimizelycmp_app.a"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: cfg(`"https://example.invalid/cb1"`, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(a, "id", name),
					resource.TestCheckResourceAttr(a, "env", "production"),
					resource.TestCheckResourceAttr(a, "expose_email", "false"),
					resource.TestCheckResourceAttr(a, "authorization_callback_urls.#", "1"),
				),
			},
			{ // adding a callback URL
				Config: cfg(`"https://example.invalid/cb1", "https://example.invalid/cb2"`, ""),
				Check:  resource.TestCheckResourceAttr(a, "authorization_callback_urls.#", "2"),
			},
			{ // callback URLs cannot be removed
				Config:      cfg(`"https://example.invalid/cb1"`, ""),
				ExpectError: regexp.MustCompile(`Callback URLs cannot be removed`),
			},
			{ // a duplicate name must fail
				Config: cfg(`"https://example.invalid/cb1", "https://example.invalid/cb2"`, fmt.Sprintf(`
resource "optimizelycmp_app" "dup" {
  name                        = %q
  homepage_url                = "https://example.invalid"
  authorization_callback_urls = ["https://example.invalid/x"]
}
`, name)),
				ExpectError: regexp.MustCompile(`already exists`),
			},
			{
				Config:       cfg(`"https://example.invalid/cb1", "https://example.invalid/cb2"`, ""),
				ResourceName: a, ImportState: true, ImportStateId: name, ImportStateVerify: true,
			},
		},
	})
}

func TestAccRoutingRule(t *testing.T) {
	accResidue(t) // the API cannot delete routing rules: destroy only forgets this one
	name := accName("rule")
	tpls, err := accClient(t).ListTemplates(context.Background(), "")
	if err != nil || len(tpls) == 0 {
		t.Skipf("no template to attach (%v)", err)
	}
	template := tpls[0].Title
	cfg := func(description, template, watchers, extra string) string {
		tpl := ""
		if template != "" {
			tpl = fmt.Sprintf("template_name = %q", template)
		}
		w := ""
		if watchers != "" {
			w = fmt.Sprintf("watchers = [%s]", watchers)
		}
		return fmt.Sprintf(`
resource "optimizelycmp_routing_rule" "r" {
  name        = %q
  description = %q
  %s
  %s
}
%s`, name, description, tpl, w, extra)
	}
	const r = "optimizelycmp_routing_rule.r"
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config: cfg("first", "", "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "id", name),
					resource.TestCheckResourceAttr(r, "description", "first"),
					// CMP adds a default watcher of its own; it must not appear in state or cause a difference
					resource.TestCheckNoResourceAttr(r, "watchers.#"),
				),
			},
			{ // change the description and attach an existing template
				Config: cfg("second", template, "", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(r, "description", "second"),
					resource.TestCheckResourceAttr(r, "template_name", template),
				),
			},
			{ // CMP silently drops a template that does not exist: the apply must say so
				Config:      cfg("second", "tfacc-no-such-template", "", ""),
				ExpectError: regexp.MustCompile(`(?s)template_name.*was not kept`),
			},
			{ // ... and an address that is not a CMP user
				Config:      cfg("second", template, `"tfacc@example.invalid"`, ""),
				ExpectError: regexp.MustCompile(`(?s)watchers.*were not kept`),
			},
			{ // clearing the template
				Config: cfg("second", "", "", ""),
				Check:  resource.TestCheckNoResourceAttr(r, "template_name"),
			},
			{ // a duplicate name must fail
				Config: cfg("second", "", "", fmt.Sprintf(`
resource "optimizelycmp_routing_rule" "dup" {
  name = %q
}
`, name)),
				ExpectError: regexp.MustCompile(`already exists`),
			},
			{
				Config:       cfg("second", "", "", ""),
				ResourceName: r, ImportState: true, ImportStateId: name, ImportStateVerify: true,
			},
		},
	})
}

func TestAccContentType(t *testing.T) {
	accResidue(t)
	userID := accFirstUserID(t, accClient(t))
	name := accName("ct")
	headline := `{
  core = { key = "headline", name = "Headline", field_type = "text-field", is_list = false, is_required = true, need_internationalization = true }
  validation_pattern = ""
  min_length = 1
}`
	featured := `{
  core = { key = "featured", name = "Featured", field_type = "boolean", is_list = false, is_required = false, need_internationalization = false }
  base_type = "boolean"
}`
	cfg := func(description string, withFeatured bool) string {
		fields := headline
		if withFeatured {
			fields += ",\n" + featured
		}
		return fmt.Sprintf(`
resource "optimizelycmp_content_type" "t" {
  name                   = %q
  description            = %q
  component              = false
  acting_user_id         = %q
  expected_locales       = ["en"]
  field_definitions_json = jsonencode([%s])
}
`, name, description, userID, fields)
	}
	var vt versionTracker
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: goneFromAPI("optimizelycmp_content_type", func(ctx context.Context, c *client.Client, id string) error {
			ct, err := c.GetContentType(ctx, id)
			if err != nil {
				return err
			}
			if !ct.Disabled {
				return fmt.Errorf("content type %s should have been disabled by destroy", id)
			}
			return client.ErrNotFound // disabled counts as gone
		}),
		Steps: []resource.TestStep{
			{
				Config: cfg("first", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_content_type.t", "name", name),
					resource.TestCheckResourceAttr("optimizelycmp_content_type.t", "disabled", "false"),
					resource.TestCheckResourceAttrWith("optimizelycmp_content_type.t", "latest_version_id", vt.remember),
				),
			},
			{ // details only: no new version, and the API's rewriting of field defaults must not show as drift
				Config: cfg("second", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_content_type.t", "description", "second"),
					resource.TestCheckResourceAttrWith("optimizelycmp_content_type.t", "latest_version_id", vt.same),
				),
			},
			{ // adding a field publishes a new version
				Config: cfg("second", true),
				Check:  resource.TestCheckResourceAttrWith("optimizelycmp_content_type.t", "latest_version_id", vt.different),
			},
			{
				Config: cfg("second", true), ResourceName: "optimizelycmp_content_type.t", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"acting_user_id", "field_definitions_json", "expected_locales"},
			},
		},
	})
}

func TestAccField(t *testing.T) {
	accResidue(t)
	name, num := accName("dropdown"), accName("currency")
	cfg := func(helper, choices string) string {
		return fmt.Sprintf(`
resource "optimizelycmp_field" "dd" {
  name        = %q
  type        = "dropdown"
  helper_text = %q
  choices = {
%s
  }
}

resource "optimizelycmp_field" "num" {
  name                   = %q
  type                   = "currency_number"
  currency_code          = "EUR"
  has_thousand_separator = true
}
`, name, helper, choices, num)
	}
	c1 := `    low  = { name = "Low" }
    high = { name = "High" }`
	c2 := `    low    = { name = "Low" }
    high   = { name = "Urgent" }
    medium = { name = "Medium" }` // rename high, add medium
	c3 := `    low  = { name = "Low", is_active = false }
    high = { name = "Urgent" }` // deactivate low, delete medium
	var highID string
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProviders,
		CheckDestroy: goneFromAPI("optimizelycmp_field", func(ctx context.Context, c *client.Client, id string) error {
			f, err := c.GetField(ctx, id)
			if err != nil {
				return err
			}
			if f.IsActive {
				return fmt.Errorf("field %s should have been deactivated by destroy", id)
			}
			return client.ErrNotFound // inactive counts as gone
		}),
		Steps: []resource.TestStep{
			{
				Config: cfg("first", c1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.%", "2"),
					resource.TestCheckResourceAttrWith("optimizelycmp_field.dd", "choices.high.id", func(v string) error { highID = v; return nil }),
					resource.TestCheckResourceAttr("optimizelycmp_field.num", "decimal_places", "2"),
					resource.TestCheckResourceAttr("optimizelycmp_field.num", "currency_code", "EUR"),
				),
			},
			{ // a rename keeps the choice's ID; a new key adds a choice
				Config: cfg("second", c2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.high.name", "Urgent"),
					resource.TestCheckResourceAttrWith("optimizelycmp_field.dd", "choices.high.id", func(v string) error {
						if v != highID {
							return fmt.Errorf("renaming recreated the choice: id %s -> %s", highID, v)
						}
						return nil
					}),
					resource.TestCheckResourceAttrSet("optimizelycmp_field.dd", "choices.medium.id"),
				),
			},
			{ // deactivating and deleting choices
				Config: cfg("second", c3),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.%", "2"),
					resource.TestCheckResourceAttr("optimizelycmp_field.dd", "choices.low.is_active", "false"),
					resource.TestCheckNoResourceAttr("optimizelycmp_field.dd", "choices.medium.name"),
				),
			},
			{Config: cfg("second", c3), ResourceName: "optimizelycmp_field.num", ImportState: true, ImportStateVerify: true},
			{Config: cfg("second", c3), ResourceName: "optimizelycmp_field.dd", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"choices"}},
		},
	})
}
