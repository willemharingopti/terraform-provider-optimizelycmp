---
page_title: "Managing folders and access"
subcategory: ""
description: |-
  Create asset library folders, nest them, and grant users and teams access.
---

# Managing folders and access

The `optimizelycmp_folder` resource manages a folder in the CMP asset library. The `optimizelycmp_folder_permission`
resource gives one user or team access to a folder.

## Create folders

A folder with no `parent_folder_id` sits at the root:

```terraform
resource "optimizelycmp_folder" "brand" {
  name = "Brand"
}
```

Nest a folder by pointing at its parent:

```terraform
resource "optimizelycmp_folder" "logos" {
  name             = "Logos"
  parent_folder_id = optimizelycmp_folder.brand.id
}
```

The provider also reports `path` (for example `/Brand/Logos`), `created_at` and `modified_at`.

### Renaming and moving

Changing `name` renames the folder in place. Changing `parent_folder_id` moves it. Removing `parent_folder_id` from the
configuration moves the folder back to the root.

### Destroying

Destroying the resource deletes the folder through the API. If a folder still holds assets, CMP may refuse or may delete
them with it: check what it contains before destroying a folder that people use.

## Grant access

Look up the user or team, then grant access:

```terraform
data "optimizelycmp_team" "marketing" {
  name = "Marketing"
}

data "optimizelycmp_user" "owner" {
  email = "jane.doe@example.com"
}

resource "optimizelycmp_folder_permission" "marketing" {
  folder_id     = optimizelycmp_folder.brand.id
  accessor_id   = data.optimizelycmp_team.marketing.id
  accessor_type = "team"
  access_type   = "edit"
}

resource "optimizelycmp_folder_permission" "owner" {
  folder_id     = optimizelycmp_folder.brand.id
  accessor_id   = data.optimizelycmp_user.owner.id
  accessor_type = "user"
  access_type   = "edit"
  is_owner      = true
}
```

- `accessor_type` is `user` or `team`. `access_type` is `view` or `edit`.
- Each `optimizelycmp_folder_permission` manages **one** accessor on one folder. It does not remove other people's access
  that was granted elsewhere.
- Changing `access_type` updates in place. Changing `folder_id`, `accessor_id` or `accessor_type` replaces the permission.
- Destroying the resource revokes that accessor's access.

### Ownership

- Only a **user** can be an owner. Setting `is_owner = true` on a team is refused.
- The API can only *set* an owner. To move ownership, set `is_owner = true` on the new owner. Changing `is_owner` from
  `true` back to `false` is refused, because a folder must always have an owner.

## Look things up

| Data source | Search by | Also returns |
|---|---|---|
| `optimizelycmp_user` | `email` | `first_name`, `last_name`, `full_name` |
| `optimizelycmp_users` | none (lists everyone) | `users`: `id`, names and `roles` for each |
| `optimizelycmp_team` | `name` | `user_ids`, `user_emails` |
| `optimizelycmp_workflow` | `name` | `description`, `is_active`, `is_flexible`, `is_smart_duration_enabled`, `is_asset_approval_enabled`, `is_resource_management_enabled`, `step_names` |
| `optimizelycmp_template` | `title` | `is_active`, `description`, `applicable_to` |
| `optimizelycmp_publishing_channel` | `name` | `disabled` |
| `optimizelycmp_label_group` | `name` | `source_org_type`, `value_names` |
| `optimizelycmp_brand_compliance_category` | `name` | `criteria_names` |
| `optimizelycmp_content_type` | `name` | `component`, `disabled` |
| `optimizelycmp_field` | `name` | `type`, `is_active`, `helper_text` |

Name and title matches are exact, ignoring case and surrounding spaces. A lookup fails if nothing matches, or if more than
one thing does, so that a configuration never silently picks the wrong object. All of them return the object's `id`.
`optimizelycmp_users` is the exception: it takes no search and returns every user, so you can filter it yourself.
