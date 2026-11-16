package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

// settingsServer mocks the token endpoint and /settings. exportNames are the webhook names GET returns, and
// dryRun is what a POST without execute=true reports. writes counts POSTs with execute=true.
func settingsServer(t *testing.T, exportNames []string, dryRun client.SettingsChangeset) (*client.Client, *int) {
	t.Helper()
	writes := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	})
	mux.HandleFunc("/v3/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			var hooks []map[string]any
			for _, n := range exportNames {
				hooks = append(hooks, map[string]any{"name": n, "description": "", "callback_url": "https://x.example", "secret": "s", "event_names": []string{"asset_added"}})
			}
			b, _ := json.Marshal(map[string]any{"resources": map[string]any{"webhooks": hooks}})
			_, _ = w.Write(b)
			return
		}
		if strings.Contains(r.URL.RawQuery, "execute=true") {
			writes++
		}
		b, _ := json.Marshal(map[string]any{"resources": map[string]any{}, "changeset": dryRun})
		_, _ = w.Write(b)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, srv.URL+"/token", "id", "secret", client.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return c, &writes
}

func changeset(create, update []string) client.SettingsChangeset {
	return client.SettingsChangeset{
		Create: map[string][]string{"webhooks": create},
		Update: map[string][]string{"webhooks": update},
	}
}

func TestGuardCreate(t *testing.T) {
	ctx := context.Background()
	item := map[string]any{"name": "hook"}

	c, _ := settingsServer(t, nil, changeset([]string{"hook"}, nil))
	if err := webhookKind.guardCreate(ctx, c, "hook", item); err != nil {
		t.Errorf("a new name should pass: %v", err)
	}
	c, _ = settingsServer(t, []string{"hook"}, changeset([]string{"hook"}, nil))
	if err := webhookKind.guardCreate(ctx, c, "hook", item); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a name in the export must be refused, got %v", err)
	}
	// export did not list it, but the API would not create it (it matches something): refuse, even with nothing to update
	c, _ = settingsServer(t, nil, changeset(nil, nil))
	if err := webhookKind.guardCreate(ctx, c, "hook", item); err == nil {
		t.Error("a dry run that does not create must be refused")
	}
}

func TestGuardUpdate(t *testing.T) {
	ctx := context.Background()
	item := map[string]any{"name": "hook"}

	c, _ := settingsServer(t, []string{"hook"}, changeset(nil, []string{"hook"}))
	if err := webhookKind.guardUpdate(ctx, c, "hook", item); err != nil {
		t.Errorf("exactly one match should pass: %v", err)
	}
	c, _ = settingsServer(t, nil, changeset([]string{"hook"}, nil))
	if err := webhookKind.guardUpdate(ctx, c, "hook", item); err == nil || !strings.Contains(err.Error(), "no longer exists") {
		t.Errorf("a missing object must be refused, got %v", err)
	}
	c, _ = settingsServer(t, []string{"hook", "hook"}, changeset(nil, []string{"hook"}))
	if err := webhookKind.guardUpdate(ctx, c, "hook", item); err == nil || !strings.Contains(err.Error(), "2 webhooks are named") {
		t.Errorf("duplicates must be refused, got %v", err)
	}
	c, _ = settingsServer(t, []string{"hook"}, changeset([]string{"hook"}, nil))
	if err := webhookKind.guardUpdate(ctx, c, "hook", item); err == nil || !strings.Contains(err.Error(), "would create a new") {
		t.Errorf("a dry run that would create must be refused, got %v", err)
	}
}

func TestGuardsNeverWrite(t *testing.T) {
	ctx := context.Background()
	c, writes := settingsServer(t, []string{"hook"}, changeset(nil, []string{"hook"}))
	_ = webhookKind.guardCreate(ctx, c, "hook", map[string]any{"name": "hook"})
	_ = webhookKind.guardUpdate(ctx, c, "hook", map[string]any{"name": "hook"})
	if *writes != 0 {
		t.Errorf("guards made %d real writes; they must only dry-run", *writes)
	}
}

func TestSetHelpers(t *testing.T) {
	if got := removedFrom([]string{"a", "b", "c"}, []string{"b"}); len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("removedFrom = %v", got)
	}
	if !sameStrings([]string{"b", "a"}, []string{"a", "b"}) || sameStrings([]string{"a"}, []string{"a", "b"}) {
		t.Error("sameStrings is wrong")
	}
}
