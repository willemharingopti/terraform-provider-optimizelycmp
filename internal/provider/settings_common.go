package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/willemharingopti/terraform-provider-optimizelycmp/internal/client"
)

// Webhooks, apps and routing rules can only be written through the bulk settings import, which matches by name,
// never deletes, and creates a new copy when several objects already share a name. settingsKind holds the checks
// that keep a Terraform create/update from doing something other than what the plan said.
type settingsKind struct {
	section string // the API section: webhooks, apps, routing_rules
	noun    string // for messages
}

func (k settingsKind) count(ctx context.Context, c *client.Client, name string) (int, error) {
	names, err := c.SettingsNames(ctx, k.section)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range names {
		if x == name {
			n++
		}
	}
	return n, nil
}

// guardCreate refuses to create when the name is taken. The import would update the existing object instead.
func (k settingsKind) guardCreate(ctx context.Context, c *client.Client, name string, item any) error {
	n, err := k.count(ctx, c, name)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("a %s named %q already exists. Import it with `terraform import <address> %q` instead of creating it", k.noun, name, name)
	}
	cs, err := c.ApplySettingsItem(ctx, k.section, item, false) // dry run: also validates the values
	if err != nil {
		return err
	}
	if !inList(cs.Create[k.section], name) {
		return fmt.Errorf("the API would not create a new %s named %q (it reported %v). Nothing was changed", k.noun, name, cs.Update[k.section])
	}
	return nil
}

// guardUpdate refuses to update unless exactly one object has the name, because with several the import creates another.
func (k settingsKind) guardUpdate(ctx context.Context, c *client.Client, name string, item any) error {
	n, err := k.count(ctx, c, name)
	if err != nil {
		return err
	}
	switch {
	case n == 0:
		return fmt.Errorf("the %s %q no longer exists in CMP; writing it would create a new one. Run terraform refresh, then apply again", k.noun, name)
	case n > 1:
		return fmt.Errorf("%d %ss are named %q. The API would create yet another one instead of updating, so Terraform will not write. Rename or remove the duplicates in CMP", n, k.noun, name)
	}
	cs, err := c.ApplySettingsItem(ctx, k.section, item, false) // dry run
	if err != nil {
		return err
	}
	if inList(cs.Create[k.section], name) {
		return fmt.Errorf("the API would create a new %s named %q instead of updating the existing one. Nothing was changed", k.noun, name)
	}
	return nil
}

func (k settingsKind) write(ctx context.Context, c *client.Client, item any) error {
	_, err := c.ApplySettingsItem(ctx, k.section, item, true)
	return err
}

// uniqueOne returns the index of the only item named name, or an error when there are several.
func (k settingsKind) onlyOne(n int, name string) error {
	if n > 1 {
		return fmt.Errorf("%d %ss are named %q; Terraform cannot tell them apart. Rename or remove the duplicates in CMP", n, k.noun, name)
	}
	return nil
}

func stringsOf(ctx context.Context, s types.Set) []string {
	var out []string
	if s.IsNull() || s.IsUnknown() {
		return []string{}
	}
	_ = s.ElementsAs(ctx, &out, false)
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

func stringSetOf(in []string) types.Set {
	elems := make([]attr.Value, len(in))
	for i, v := range in {
		elems[i] = types.StringValue(v)
	}
	s, _ := types.SetValue(types.StringType, elems)
	return s
}

// removedFrom returns the members of have that are missing from want, sorted.
func removedFrom(have, want []string) []string {
	keep := map[string]bool{}
	for _, w := range want {
		keep[w] = true
	}
	var out []string
	for _, h := range have {
		if !keep[h] {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}

func sameStrings(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
