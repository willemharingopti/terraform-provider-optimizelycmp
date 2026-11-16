---
page_title: "Reading settings"
subcategory: ""
description: |-
  Read an export of an organisation's configuration, and what it does and does not contain.
---

# Reading settings

The `optimizelycmp_settings` data source reads the API's settings export (`GET /settings`, marked experimental by CMP). It
changes nothing. It returns the organisation's configuration in seven sections, each as JSON text in the API's own shape:

| Attribute | Holds |
|---|---|
| `labels_json` | label groups and their labels |
| `custom_fields_json` | custom fields, an object keyed by field type (`currency`, `dropdown`, ...) |
| `workflows_json` | workflows with steps, labels, custom fields and channels |
| `templates_json` | templates with form fields, sections and instructions |
| `apps_json` | apps |
| `webhooks_json` | webhooks (**sensitive**) |
| `routing_rules_json` | routing rules |

Read a section with `jsondecode()`:

```terraform
data "optimizelycmp_settings" "current" {}

locals {
  workflow_names = [for w in jsondecode(data.optimizelycmp_settings.current.workflows_json) : w.name]
}
```

## Secrets

The export includes each webhook's **secret in full**, so `webhooks_json` is marked sensitive and hidden in plan output.
That does not keep it out of Terraform state: data source results are written to state, so the secrets are stored there in
plain text. Protect your state accordingly, and use the other sections if you do not need webhooks. To use `webhooks_json`
you need `nonsensitive()`, which is your decision to reveal it.

## What the export does not tell you

The export is not a complete listing of the account:

- **Workflows:** in one account the export held 3 workflows while the workflow list endpoint (`optimizelycmp_workflow`) found 5.
- **Custom fields:** inactive fields are not included.
- **Names differ:** label groups appear by a slug (`content_pillar`), not the display name that `optimizelycmp_label_group`
  shows ("Content Pillar").
- **No IDs:** items are identified by name only. Use the dedicated data sources when you need an ID.

Treat it as a snapshot of the active configuration, useful for review and for comparing two accounts, not as the source of
truth for every object.

## Writing settings

The same API can also create or update configuration in bulk (`POST /settings`), and the provider uses it for
[`optimizelycmp_label_group`](../resources/label_group). To manage a group that already exists, import it by its display name:
`terraform import optimizelycmp_label_group.pillar "Content Pillar"`, then write a `labels` map that matches it. Things worth knowing about that endpoint, found by testing it:

- **Everything is matched by name and nothing is ever deleted.** An existing item with the same name is updated, otherwise
  one is created. There is no rename and no removal.
- **Merging:** labels are only ever added to a group or overwritten by name. A label cannot be taken away through the API.
- **Dry run:** unless `execute=true` is sent, the endpoint changes nothing and only reports what it would create or update.
  The provider uses this to refuse to "create" a label group that already exists.
- **Request shape:** the body must be wrapped in a top-level `resources` key, and every section must be present (empty is
  fine). The published API reference does not show the wrapper.
- **Names:** the API knows a group by its `label_type`. For a group created in the CMP interface that is a slug of the
  display name (`content_pillar` for "Content Pillar"), and writing the display name would create a *second* group. The
  provider resolves this for you: it writes with the export's own spelling, and refuses to create a group whose name
  already exists in either spelling.
- **Colours:** label colours are checked. Only eight of the nine listed colours are accepted, in upper case.

## Webhooks, apps and routing rules

`optimizelycmp_webhook`, `optimizelycmp_app` and `optimizelycmp_routing_rule` use the same endpoint and follow the same rules:

- **The name is the identity.** There is no ID, so `id` is the name. Changing the name creates a new object and leaves the old one
  in CMP.
- **Nothing is ever deleted.** Destroying the resource only removes it from Terraform state, with a warning. A destroyed webhook
  keeps receiving events, and an app stays registered. Remove them in the CMP interface.
- **Merge-only attributes cannot shrink.** A webhook's `event_names` and an app's `authorization_callback_urls` can only grow;
  removing one fails at plan time.
- **Duplicates are refused.** The API creates another copy when several objects share a name. The provider checks first and will
  not create a name that exists (import it instead), nor update a name that is ambiguous. It also dry-runs every write.
- **Written values are checked.** After every write the object is read back, and the apply fails if CMP kept something different
  from what you configured.
- **Import by name:** `terraform import optimizelycmp_webhook.tasks "Task events"`.

What the API accepted when tested (it validates, and a dry run reports problems without changing anything):

| Object | Rules |
|---|---|
| Webhook | `callback_url` https only; secret not empty; `event_names` limited to the events below |
| App | `homepage_url` and callback URLs https only; at least one callback URL; `env` must be `production` |
| Routing rule | `field_type` must be `dueDate`, `label` or `customField`; label and customField rules need an `identifier` and accepted operator `equal`; customField also needs a `custom_field_type` of `dropdown`, `multichoice`, `multi_select_dropdown` or `checkbox` |

**Webhook events.** The settings import accepts only part of the events CMP offers. Accepted: `asset_added`, `asset_modified`,
`asset_removed`, `external_sub_step_comment_added`, `external_sub_step_comment_modified`, `external_sub_step_comment_removed`,
`external_sub_step_completed`, `external_sub_step_modified`, `external_sub_step_started`, `task_asset_added`,
`task_asset_draft_added`, `task_asset_draft_removed`, `task_asset_modified`, `task_asset_removed`, `task_brief_added`,
`task_brief_modified`, `task_brief_removed`, `task_custom_field_modified`, `task_metadata_modified` and `task_removed`.
Rejected, although they exist (a webhook made in the CMP interface can have them): `task_added`, campaign, work request and
workflow sub-step events, `content_preview_requested`, `event_*` and `asset_renditions_created`. A webhook that already has a
rejected event can be imported, but cannot be updated through this resource while its configuration lists one.

**Things CMP does silently.** The API does not report these, so the provider reads each object back after writing and fails the
apply, naming the problem:

- A routing rule's `template_name` that does not exist is dropped. Unsetting it clears the template.
- A routing rule watcher that is not a CMP user is dropped. CMP also adds a default watcher on its own, which the provider
  ignores. Watchers cannot be removed as far as tested, so removing one from the configuration fails at plan time. Watchers
  added in CMP outside Terraform are ignored.
- Due date conditions could not be made to pass: no operator I tried was accepted. If you know the right combination, the
  resource passes the values through and lets the API decide.

### Secrets

A webhook's secret is stored in Terraform state, because the API returns it in full and the provider reads it back to detect
changes. Pass it as a sensitive variable rather than writing it in configuration, and protect your state.

Templates and workflows can also only be written through this endpoint and are not managed by Terraform yet.
