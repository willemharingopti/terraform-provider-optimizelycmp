---
page_title: "Getting started"
subcategory: ""
description: |-
  Install the provider, authenticate against Optimizely CMP, and run a first read-only configuration.
---

# Getting started

This provider manages parts of an Optimizely CMP account through its
[Open REST API (v3)](https://docs.optimizely.com/content-marketing-platform/api): asset library folders and who can
access them, plus read-only lookups of users, teams, workflows, templates and publishing channels.

## Before you start

You need:

- An Optimizely CMP account. **Use a sandbox or a test area first**: applying a configuration creates, changes and
  deletes real objects.
- API credentials: a *client ID* and *client secret* for your CMP account. The provider uses the OAuth 2.0
  client-credentials flow, so no browser login is involved.
- Terraform 1.0 or later.

## Install

Declare the provider in your configuration:

```terraform
terraform {
  required_providers {
    optimizelycmp = {
      source = "willemharingopti/optimizelycmp" # replace with the address you publish under
    }
  }
}
```

To try a local build instead, run `make install` in the provider's repository. This copies the binary into Terraform's
local plugin directory so no registry is needed. After rebuilding, delete `.terraform.lock.hcl` and `.terraform/` in your
configuration and run `terraform init` again.

## Configure credentials

Export the credentials in the shell that runs Terraform:

```sh
export OPTIMIZELY_CMP_CLIENT_ID=...
export OPTIMIZELY_CMP_CLIENT_SECRET=...
```

Then use an empty provider block:

```terraform
provider "optimizelycmp" {}
```

See [Authentication and security](authentication) for the details.

## A first read-only configuration

This only reads. It looks up a team and prints its ID:

```terraform
data "optimizelycmp_team" "marketing" {
  name = "Marketing"
}

output "marketing_team_id" {
  value = data.optimizelycmp_team.marketing.id
}
```

Run `terraform init` and `terraform apply`. If it prints an ID, authentication works.

## Next steps

- [Managing folders and access](managing-folders) creates folders and controls who can use them.
- [Importing existing objects](importing-existing-objects) brings folders you already have under Terraform.
- [Troubleshooting](troubleshooting) lists common errors.
