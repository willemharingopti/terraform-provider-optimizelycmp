---
page_title: "Importing existing objects"
subcategory: ""
description: |-
  Bring folders and permissions that already exist in CMP under Terraform management.
---

# Importing existing objects

Folders and folder permissions that were created in the CMP interface can be adopted with `terraform import`, so
Terraform manages them from then on without recreating them.

## Folders

Find the folder's ID (it is in the folder's API URL, `.../v3/folders/<id>`), write a matching resource, and import:

```terraform
resource "optimizelycmp_folder" "brand" {
  name = "Brand"
}
```

```sh
terraform import optimizelycmp_folder.brand <folder_id>
```

Or use an `import` block:

```terraform
import {
  to = optimizelycmp_folder.brand
  id = "<folder_id>"
}
```

## Folder permissions

The import ID is `<folder_id>/<accessor_id>`, where the accessor is the user or team:

```sh
terraform import optimizelycmp_folder_permission.marketing <folder_id>/<team_id>
```

The access level, accessor type and ownership are read from the API.

## Check for drift

After importing, run `terraform plan`. Any difference shows where your configuration disagrees with CMP: change the
configuration to match, or apply to bring CMP in line. Changes made in the CMP interface later show up the same way, as
a plan that wants to put things back.

A folder or permission deleted in CMP is removed from state on the next `plan` or `apply`, and Terraform then plans to
recreate it.
