---
page_title: "Troubleshooting"
subcategory: ""
description: |-
  Common errors, what they mean, and how to fix them.
---

# Troubleshooting

## Errors from the provider or Terraform

**`client_id and client_secret are required`**
: The environment variables are not set in the shell that runs Terraform. Terraform does not read `.env` files; export the
  variables (see [Authentication and security](authentication)).

**`token request failed with status 401`**
: CMP rejected the client ID or secret. Check for stray spaces or quotes, and that the secret has not been regenerated.
  The response body is deliberately not shown because it can echo credentials.

**`the local package ... doesn't match any of the checksums previously recorded in the dependency lock file`**
: You rebuilt a locally installed provider without changing its version. Run `rm -rf .terraform .terraform.lock.hcl`, then
  `terraform init`.

**`nothing found matching "..."` / `N matches for "..."`**
: A data source lookup found no object, or several. Names and titles must match exactly (ignoring case). Check the spelling,
  and that the API client can see the object.

## Errors from the API

| Error | Meaning and what to do |
|---|---|
| `401` | The token was refused. The provider already retried once with a fresh token, so check the credentials. |
| `403` | The API client is not allowed to do this. Ask a CMP administrator to widen its access. |
| `404` on create | A referenced object does not exist, for example a `parent_folder_id` or `folder_id` that was deleted. |
| `400 ... unsupported field 'is_owner' for 'team'` | Teams cannot own a folder. Remove `is_owner`, or grant the owner role to a user. |
| `422` on a permission change | The API refused the change, for example because of ownership rules. Read the message in the error. To move ownership, make another user the owner first. |
| `429` | CMP rate-limited the request. The provider waits and retries up to three times, honouring `Retry-After`. |

## Ownership changes are refused

`is_owner` can only change from `false` to `true`. To move ownership, set `is_owner = true` on the new owner. See
[Managing folders and access](managing-folders).

## Getting more detail

Run Terraform with `TF_LOG=DEBUG` to see provider activity. Credentials are not logged, but check the output before you
share it.

For access problems or API client changes in CMP, ask your CMP administrator.
