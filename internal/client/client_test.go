package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient starts a mock CMP API (token endpoint + handler) and returns a client for it.
func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *int32) {
	t.Helper()
	var tokens int32
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tokens, 1)
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_id") != "id" || r.Form.Get("client_secret") != "secret" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/v3/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, srv.URL+"/token", "id", "secret", WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c, &tokens
}

func TestNewValidation(t *testing.T) {
	if _, err := New("http://insecure.example", "", "id", "secret"); err == nil {
		t.Error("http host accepted")
	}
	if _, err := New("", "", "", ""); err == nil {
		t.Error("missing credentials accepted")
	}
	c, err := New("https://api.cmp.optimizely.com/v3/", "", "id", "secret")
	if err != nil || c.baseURL != "https://api.cmp.optimizely.com/v3" {
		t.Errorf("base URL not normalised: %v %v", c, err)
	}
}

func TestFolderCRUDAndTokenReuse(t *testing.T) {
	c, tokens := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/folders":
			var in map[string]any
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in["name"] != "icons" || in["parent_folder_id"] != nil {
				t.Errorf("unexpected create body %v", in)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"f1","name":"icons","parent_folder_id":null,"path":"/icons"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v3/folders/f1":
			_, _ = w.Write([]byte(`{"id":"f1","name":"icons","parent_folder_id":"p","path":"/p/icons"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v3/folders/f1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	f, err := c.CreateFolder(ctx, "icons", nil)
	if err != nil || f.ID != "f1" {
		t.Fatalf("create: %+v %v", f, err)
	}
	if f, err = c.GetFolder(ctx, "f1"); err != nil || f.ParentFolderID == nil || *f.ParentFolderID != "p" {
		t.Fatalf("get: %+v %v", f, err)
	}
	if err := c.DeleteFolder(ctx, "f1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetFolder(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := c.GetFolder(ctx, ""); err == nil {
		t.Fatal("empty id accepted")
	}
	if n := atomic.LoadInt32(tokens); n != 1 {
		t.Errorf("token fetched %d times, want 1", n)
	}
}

func TestFolderUpdateSendsNullParent(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var in map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&in)
		if string(in["parent_folder_id"]) != "null" {
			t.Errorf("parent_folder_id must be sent as null to move to root, body %v", in)
		}
		_, _ = w.Write([]byte(`{"id":"f1","name":"n","parent_folder_id":null,"path":"/n"}`))
	})
	if _, err := c.UpdateFolder(context.Background(), "f1", "n", nil); err != nil {
		t.Fatal(err)
	}
}

func TestPaginationAndPermissionLookup(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		off := r.URL.Query().Get("offset")
		if off == "0" {
			var data []map[string]any
			for i := 0; i < pageSize; i++ {
				data = append(data, map[string]any{"id": fmt.Sprintf("u%d", i), "type": "user", "access_type": "view"})
			}
			b, _ := json.Marshal(map[string]any{"data": data, "pagination": map[string]any{"next": "https://x/next"}})
			_, _ = w.Write(b)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"team9","type":"team","access_type":"edit","is_owner":false}],"pagination":{"next":null}}`))
	})
	p, err := c.GetFolderPermission(context.Background(), "f1", "team9")
	if err != nil || p.AccessType != "edit" || p.Type != "team" {
		t.Fatalf("got %+v %v", p, err)
	}
	if _, err := c.GetFolderPermission(context.Background(), "f1", "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGrantPermissionBody(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Type        string `json:"type"`
			Permissions []struct {
				ID         string `json:"id"`
				AccessType string `json:"access_type"`
				IsOwner    bool   `json:"is_owner"`
			} `json:"permissions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/v3/folders/f1/permissions" || in.Type != "user" || len(in.Permissions) != 1 || in.Permissions[0].ID != "u1" || !in.Permissions[0].IsOwner {
			t.Errorf("unexpected request %s %+v", r.URL.Path, in)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.GrantFolderPermission(context.Background(), "f1", "u1", "user", "edit", true); err != nil {
		t.Fatal(err)
	}
}

func TestGrantPermissionOmitsIsOwnerWhenFalse(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var in map[string][]map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if _, ok := in["permissions"][0]["is_owner"]; ok {
			t.Errorf("is_owner must be omitted for a non-owner grant: %v", in)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.GrantFolderPermission(context.Background(), "f1", "t1", "team", "view", false); err != nil {
		t.Fatal(err)
	}
}

func TestFindUserByEmailFollowsRedirect(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/users":
			if r.URL.Query().Get("email") != "a+b@example.com" {
				t.Errorf("email not escaped/passed: %q", r.URL.RawQuery)
			}
			w.Header().Set("Location", "https://api.cmp.optimizely.com/v3/users/u42")
			w.WriteHeader(http.StatusFound)
		case "/v3/users/u42":
			_, _ = w.Write([]byte(`{"id":"u42","first_name":"A","last_name":"B","full_name":"A B","email":"a+b@example.com"}`))
		default:
			http.NotFound(w, r)
		}
	})
	u, err := c.FindUserByEmail(context.Background(), "a+b@example.com")
	if err != nil || u.ID != "u42" || u.FullName != "A B" {
		t.Fatalf("got %+v %v", u, err)
	}
}

func TestAPIErrorAndTokenFailure(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"nope"}`, http.StatusForbidden)
	})
	var apiErr *APIError
	if _, err := c.GetFolder(context.Background(), "f1"); !errors.As(err, &apiErr) || apiErr.Status != 403 {
		t.Fatalf("want 403 APIError, got %v", err)
	}
	bad, err := New(c.baseURL, c.tokenURL, "id", "wrong", WithHTTPClient(c.http))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bad.GetFolder(context.Background(), "f1"); err == nil {
		t.Fatal("bad credentials accepted")
	}
}

func TestContentTypeLifecycle(t *testing.T) {
	var gotCreate, gotVersion, gotUpdate map[string]any
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/structured-content/content-types":
			gotCreate = in
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"created":true,"content_type_guid":"ct1","content_type_version_guid":"v1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v3/structured-content/content-types/ct1/versions":
			gotVersion = in
			_, _ = w.Write([]byte(`{"created":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v3/structured-content/content-types/ct1":
			gotUpdate = in
			_, _ = w.Write([]byte(`{"updated":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v3/structured-content/content-types/ct1":
			_, _ = w.Write([]byte(`{"content_type_guid":"ct1","name":"Article","component":false,"disabled":false,"description":null,
				"latest_version":{"version_guid":"v1","latest":true,"expected_locales":["en"],"field_definitions":[{"core":{"name":"title"}}]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v3/structured-content/content-types":
			if r.URL.Query().Get("list") != "3" {
				t.Errorf("list option missing: %q", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`[{"content_type_guid":"ct1","name":"Article","component":false,"disabled":false}]`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	defs := json.RawMessage(`[{"core":{"name":"title"}}]`)
	id, err := c.CreateContentType(ctx, ContentTypeDetails{Name: "Article"}, defs, []string{"en"}, "u1")
	if err != nil || id != "ct1" {
		t.Fatalf("create: %q %v", id, err)
	}
	if gotCreate["created_by"] != "u1" || gotCreate["details"].(map[string]any)["name"] != "Article" || gotCreate["expected_locales"] == nil {
		t.Errorf("create body %v", gotCreate)
	}
	ct, err := c.GetContentType(ctx, "ct1")
	if err != nil || ct.LatestVersion == nil || ct.LatestVersion.VersionGUID != "v1" || ct.Description != nil {
		t.Fatalf("get: %+v %v", ct, err)
	}
	if err := c.UpdateContentType(ctx, "ct1", ContentTypeDetails{Name: "Article", Disabled: true}, "u2"); err != nil {
		t.Fatal(err)
	}
	if gotUpdate["updated_by"] != "u2" || gotUpdate["details"].(map[string]any)["disabled"] != true {
		t.Errorf("update body %v", gotUpdate)
	}
	if err := c.AddContentTypeVersion(ctx, "ct1", defs, nil, "u2"); err != nil {
		t.Fatal(err)
	}
	if _, has := gotVersion["expected_locales"]; has || gotVersion["created_by"] != "u2" {
		t.Errorf("version body %v", gotVersion)
	}
	list, err := c.ListContentTypes(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
}

func TestFieldsCRUD(t *testing.T) {
	var created, patched, added map[string]any
	var addedList []map[string]any
	deleted := ""
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v3/fields":
			_ = json.NewDecoder(r.Body).Decode(&created)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"f1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v3/fields":
			if r.URL.Query().Get("ids") == "f1" {
				_, _ = w.Write([]byte(`{"data":[{"id":"f1","name":"Priority","type":"dropdown","is_active":true,"helper_text":"",
					"is_multi_select":false,"choices":[{"id":"c1","name":"Low","is_active":true}]}],"pagination":{"next":null,"previous":null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[],"pagination":{"next":null,"previous":null}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v3/fields/f1":
			_ = json.NewDecoder(r.Body).Decode(&patched)
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/v3/fields/f1/choices":
			_ = json.NewDecoder(r.Body).Decode(&addedList)
			_ = added
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`[]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v3/fields/f1/choices/c1":
			deleted = "c1"
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	id, err := c.CreateField(ctx, map[string]any{"name": "Priority", "type": "dropdown"})
	if err != nil || id != "f1" || created["type"] != "dropdown" {
		t.Fatalf("create: %q %v %v", id, err, created)
	}
	f, err := c.GetField(ctx, "f1")
	if err != nil || f.Type != "dropdown" || len(f.Choices) != 1 || f.Choices[0].ID != "c1" || f.IsMultiSelect == nil {
		t.Fatalf("get: %+v %v", f, err)
	}
	if _, err := c.GetField(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := c.UpdateField(ctx, "f1", map[string]any{"type": "dropdown", "is_active": false}); err != nil || patched["is_active"] != false || patched["type"] != "dropdown" {
		t.Fatalf("update: %v %v", err, patched)
	}
	if err := c.AddFieldChoices(ctx, "f1", []map[string]any{{"name": "High"}}); err != nil || len(addedList) != 1 || addedList[0]["name"] != "High" {
		t.Fatalf("add choices: %v %v", err, addedList)
	}
	if err := c.DeleteFieldChoice(ctx, "f1", "c1"); err != nil || deleted != "c1" {
		t.Fatalf("delete choice: %v", err)
	}
}

func TestDetailLookups(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/teams/t1":
			_, _ = w.Write([]byte(`{"id":"t1","name":"Legal","users":[{"id":"u1","name":"A","email":"a@x.com"}]}`))
		case "/v3/workflows/w1":
			_, _ = w.Write([]byte(`{"id":"w1","name":"Flow","description":null,"is_active":true,"is_flexible":false,"is_smart_duration_enabled":true,
				"is_asset_approval_enabled":false,"is_resource_management_enabled":true,"steps":[{"id":"s1","name":"Draft","sub_steps":[]}],"fields":[{"id":"f","type":"x"}]}`))
		case "/v3/templates/p1":
			_, _ = w.Write([]byte(`{"id":"p1","title":"Brief","description":"d","is_active":true,"applicable_to":["task_brief"],"form_fields":[]}`))
		case "/v3/userlist":
			_, _ = w.Write([]byte(`{"data":[{"id":"u1","first_name":"A","last_name":"B","full_name":"A B","roles":[{"name":"Creator"}]}],"pagination":{"next":null,"previous":null}}`))
		case "/v3/label-groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"g1","name":"Pillars","source_org_type":"current","values":[{"id":"v1","name":"One"}]}],"pagination":{"next":null,"previous":null}}`))
		case "/v3/brand-compliance/categories":
			_, _ = w.Write([]byte(`{"data":[{"id":"b1","name":"Tone","criteria":[{"id":"c1","name":"Friendly"}]}],"pagination":{"next":null,"previous":null}}`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if tm, err := c.GetTeam(ctx, "t1"); err != nil || len(tm.Users) != 1 || tm.Users[0].Email != "a@x.com" {
		t.Errorf("team: %+v %v", tm, err)
	}
	if wf, err := c.GetWorkflow(ctx, "w1"); err != nil || !wf.IsActive || wf.IsFlexible || !wf.IsSmartDurationEnabled || len(wf.Steps) != 1 || wf.Description != nil {
		t.Errorf("workflow: %+v %v", wf, err)
	}
	if tp, err := c.GetTemplate(ctx, "p1"); err != nil || tp.Description != "d" || len(tp.ApplicableTo) != 1 {
		t.Errorf("template: %+v %v", tp, err)
	}
	if us, err := c.ListUsers(ctx); err != nil || len(us) != 1 || us[0].Roles[0].Name != "Creator" {
		t.Errorf("users: %+v %v", us, err)
	}
	if lg, err := c.ListLabelGroups(ctx); err != nil || len(lg) != 1 || lg[0].Values[0].Name != "One" {
		t.Errorf("label groups: %+v %v", lg, err)
	}
	if bc, err := c.ListBrandComplianceCategories(ctx); err != nil || len(bc) != 1 || bc[0].Criteria[0].Name != "Friendly" {
		t.Errorf("categories: %+v %v", bc, err)
	}
}

func TestGetSettings(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/settings" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"resources":{"labels":[{"label_type":"Pillar","has_single_value":true,"labels":[]}],
			"custom_fields":{"currency":[{"label":"Budget"}],"dropdown":[]},"webhooks":[{"name":"w","secret":"s"}]}}`))
	})
	res, err := c.GetSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var labels []map[string]any
	if err := json.Unmarshal(res["labels"], &labels); err != nil || len(labels) != 1 || labels[0]["label_type"] != "Pillar" {
		t.Errorf("labels: %s %v", res["labels"], err)
	}
	var cf map[string][]any
	if err := json.Unmarshal(res["custom_fields"], &cf); err != nil || len(cf["currency"]) != 1 {
		t.Errorf("custom_fields: %s %v", res["custom_fields"], err)
	}
	if _, ok := res["workflows"]; ok {
		t.Error("absent section should be absent")
	}
}

func TestUpsertLabelGroup(t *testing.T) {
	var bodies []map[string]any
	var queries []string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/settings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		bodies = append(bodies, in)
		queries = append(queries, r.URL.RawQuery)
		_, _ = w.Write([]byte(`{"resources":{},"changeset":{"create":{"labels":["grp"]},"update":{"labels":[]}}}`))
	})
	ctx := context.Background()
	cs, err := c.UpsertLabelGroup(ctx, "grp", true, []Label{{Name: "One", Color: "#4ECFD5"}}, false)
	if err != nil || len(cs.Create["labels"]) != 1 || cs.Create["labels"][0] != "grp" {
		t.Fatalf("dry run: %+v %v", cs, err)
	}
	if _, err := c.UpsertLabelGroup(ctx, "grp", true, nil, true); err != nil {
		t.Fatal(err)
	}
	if queries[0] != "" || queries[1] != "execute=true" {
		t.Errorf("execute flag: %q", queries)
	}
	res, ok := bodies[0]["resources"].(map[string]any)
	if !ok {
		t.Fatalf("body must be wrapped in resources: %v", bodies[0])
	}
	for _, s := range SettingsSections {
		if _, ok := res[s]; !ok {
			t.Errorf("section %q missing: the API requires all of them", s)
		}
	}
	cf, _ := res["custom_fields"].(map[string]any)
	if len(cf) != len(customFieldTypes) {
		t.Errorf("custom_fields must list all %d types, got %d", len(customFieldTypes), len(cf))
	}
	g := res["labels"].([]any)[0].(map[string]any)
	if g["label_type"] != "grp" || g["has_single_value"] != true {
		t.Errorf("group: %v", g)
	}
	if ls, _ := bodies[1]["resources"].(map[string]any)["labels"].([]any)[0].(map[string]any)["labels"].([]any); ls == nil {
		t.Error("labels must be an empty list, not null")
	}
}

func TestSettingsObjectExports(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"resources":{
			"webhooks":[{"name":"w","description":"d","callback_url":"https://x","secret":"s","event_names":["asset_added"]}],
			"apps":[{"name":"a","description":"","homepage_url":"https://h","authorization_callback_urls":["https://cb"],"expose_email":true,"env":"production"}],
			"routing_rules":[{"name":"r","description":"d","template_name":null,"watchers":["x@y.z"],
				"rules":[{"field_type":"label","identifier":"g","operator":"equal","value":["v"],"unit":null,"custom_field_type":null}]}]}}`))
	})
	ctx := context.Background()
	if ws, err := c.ExportWebhooks(ctx); err != nil || len(ws) != 1 || ws[0].Secret != "s" || ws[0].EventNames[0] != "asset_added" {
		t.Errorf("webhooks: %+v %v", ws, err)
	}
	if as, err := c.ExportApps(ctx); err != nil || len(as) != 1 || !as[0].ExposeEmail || as[0].AuthorizationCallbackURL[0] != "https://cb" {
		t.Errorf("apps: %+v %v", as, err)
	}
	rs, err := c.ExportRoutingRules(ctx)
	if err != nil || len(rs) != 1 || rs[0].TemplateName != nil || rs[0].Rules[0].Unit != nil || rs[0].Rules[0].Identifier == nil || *rs[0].Rules[0].Identifier != "g" {
		t.Errorf("routing rules: %+v %v", rs, err)
	}
	if names, err := c.SettingsNames(ctx, "apps"); err != nil || len(names) != 1 || names[0] != "a" {
		t.Errorf("names: %v %v", names, err)
	}
	if names, err := c.SettingsNames(ctx, "templates"); err != nil || len(names) != 0 {
		t.Errorf("absent section should be empty: %v %v", names, err)
	}
}

func TestRedirectsAreNotSuccess(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://elsewhere.invalid/x")
		w.WriteHeader(http.StatusFound)
	})
	var out map[string]any
	err := c.Do(context.Background(), http.MethodGet, "/folders/f1", nil, &out)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusFound {
		t.Fatalf("a 302 outside the user lookup must be an error, got %v", err)
	}
}

func TestOversizedResponseIsRefused(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(append([]byte(`{"id":"`), make([]byte, maxResponseBytes)...))
	})
	var out map[string]any
	if err := c.Do(context.Background(), http.MethodGet, "/folders/f1", nil, &out); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("an oversized response must be refused with a clear error, got %v", err)
	}
}

func TestPaginationHasAnEnd(t *testing.T) {
	calls := 0
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"data":[{"id":"x","name":"n"}],"pagination":{"next":"https://x/more"}}`)) // never ends
	})
	if _, err := c.ListTeams(context.Background()); err == nil || !strings.Contains(err.Error(), "did not end") {
		t.Fatalf("endless pagination must stop with an error, got %v", err)
	}
	if calls > maxPages {
		t.Errorf("made %d calls, more than the cap of %d", calls, maxPages)
	}
}

func TestSettingsExportIsCachedAndClearedByWrites(t *testing.T) {
	old := settingsCacheTTL
	settingsCacheTTL = time.Hour
	t.Cleanup(func() { settingsCacheTTL = old })
	var gets, posts int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v3/settings" && r.Method == http.MethodGet:
			atomic.AddInt32(&gets, 1)
			_, _ = w.Write([]byte(`{"resources":{"webhooks":[{"name":"w"}]}}`))
		case r.URL.Path == "/v3/settings" && r.Method == http.MethodPost:
			atomic.AddInt32(&posts, 1)
			_, _ = w.Write([]byte(`{"resources":{},"changeset":{"create":{},"update":{}}}`))
		case r.URL.Path == "/v3/fields" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"f1"}`))
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	read := func() int32 {
		for i := 0; i < 5; i++ {
			if _, err := c.SettingsNames(ctx, "webhooks"); err != nil {
				t.Fatal(err)
			}
		}
		return atomic.LoadInt32(&gets)
	}
	if got := read(); got != 1 {
		t.Fatalf("5 reads should download the export once, downloaded %d times", got)
	}
	// a dry run changes nothing, so it must not throw the cache away
	if _, err := c.ApplySettingsItem(ctx, "webhooks", map[string]any{"name": "w"}, false); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != 1 {
		t.Errorf("a dry run cleared the cache (%d downloads)", got)
	}
	// a real import does
	if _, err := c.ApplySettingsItem(ctx, "webhooks", map[string]any{"name": "w"}, true); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != 2 {
		t.Errorf("an import should clear the cache (%d downloads)", got)
	}
	// and so does a write to anything else, because fields show up in the export
	if _, err := c.CreateField(ctx, map[string]any{"name": "f", "type": "text"}); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != 3 {
		t.Errorf("creating a field should clear the cache (%d downloads)", got)
	}
}

func TestSettingsExportCacheExpiresAndIsSafeToShare(t *testing.T) {
	old := settingsCacheTTL
	settingsCacheTTL = 20 * time.Millisecond
	t.Cleanup(func() { settingsCacheTTL = old })
	var gets int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&gets, 1)
		_, _ = w.Write([]byte(`{"resources":{"apps":[{"name":"a"}]}}`))
	})
	ctx := context.Background()
	first, err := c.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first["apps"][0] = 'X' // a caller scribbling on its copy must not corrupt the cache
	second, _ := c.GetSettings(ctx)
	if string(second["apps"]) != `[{"name":"a"}]` {
		t.Errorf("cache was modified through a returned copy: %s", second["apps"])
	}
	time.Sleep(40 * time.Millisecond)
	_, _ = c.GetSettings(ctx)
	if got := atomic.LoadInt32(&gets); got != 2 {
		t.Errorf("want 2 downloads (one before and one after expiry), got %d", got)
	}
	// parallel readers share one download
	time.Sleep(40 * time.Millisecond)
	atomic.StoreInt32(&gets, 0)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = c.GetSettings(ctx) }()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&gets); got != 1 {
		t.Errorf("20 parallel readers made %d downloads, want 1", got)
	}
}
