package client

import (
	"context"
	"os"
	"testing"
)

// TestLiveReadOnly checks the token request and a few list endpoints against a real CMP account.
// It only reads, and is skipped unless OPTIMIZELY_CMP_CLIENT_ID and OPTIMIZELY_CMP_CLIENT_SECRET are set.
func TestLiveReadOnly(t *testing.T) {
	id, secret := os.Getenv("OPTIMIZELY_CMP_CLIENT_ID"), os.Getenv("OPTIMIZELY_CMP_CLIENT_SECRET")
	if id == "" || secret == "" {
		t.Skip("no CMP credentials in the environment")
	}
	c, err := New(os.Getenv("OPTIMIZELY_CMP_BASE_URL"), os.Getenv("OPTIMIZELY_CMP_TOKEN_URL"), id, secret)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	teams, err := c.ListTeams(ctx)
	if err != nil {
		t.Fatalf("teams: %v", err)
	}
	t.Logf("teams: %d", len(teams))
	wf, err := c.ListWorkflows(ctx)
	if err != nil {
		t.Fatalf("workflows: %v", err)
	}
	t.Logf("workflows: %d", len(wf))
	ch, err := c.ListPublishingChannels(ctx)
	if err != nil {
		t.Fatalf("publishing channels: %v", err)
	}
	t.Logf("publishing channels: %d", len(ch))
	folders, err := c.ListFolders(ctx, "")
	if err != nil {
		t.Fatalf("folders: %v", err)
	}
	t.Logf("root folders: %d", len(folders))
	if len(folders) > 0 {
		perms, err := c.ListFolderPermissions(ctx, folders[0].ID)
		if err != nil {
			t.Fatalf("folder permissions: %v", err)
		}
		t.Logf("permissions on first folder: %d", len(perms))
	}
}
