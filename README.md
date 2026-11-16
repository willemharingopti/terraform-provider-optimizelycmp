# Terraform provider for Optimizely CMP

Skeleton of a Terraform provider for Optimizely CMP, built on the Terraform Plugin Framework and laid out like
[cms-terraform](../cms-terraform), built against the CMP Open API v3 (`openapi.json`, kept locally and git-ignored).

> **Status: early.** Covered by unit tests against a mock API only. The OAuth client-credentials token request and the
> endpoints have not yet been run against a live CMP account, so try it on a sandbox first.

## What it manages

| Type | Name | Notes |
|---|---|---|
| Resource | `optimizelycmp_folder` | asset library folder; supports `terraform import` by ID |
| Resource | `optimizelycmp_content_type` | structured content type and its fields (experimental API; destroy disables it) |
| Resource | `optimizelycmp_label_group` | label group and its labels, via the bulk settings import (labels can be added, not removed; destroy only forgets it) |
| Resource | `optimizelycmp_webhook` | webhook via the bulk settings import (events can be added, not removed; secret stored in state; destroy only forgets it) |
| Resource | `optimizelycmp_app` | app via the bulk settings import (callback URLs can be added, not removed; destroy only forgets it) |
| Resource | `optimizelycmp_routing_rule` | routing rule via the bulk settings import (CMP silently drops unknown templates and non-user watchers; destroy only forgets it) |
| Resource | `optimizelycmp_field` | custom field with its choices (experimental API; destroy deactivates it) |
| Resource | `optimizelycmp_folder_permission` | one user or team's access to a folder; import `<folder_id>/<accessor_id>` |
| Data source | `optimizelycmp_settings` | read-only export of labels, custom fields, workflows, templates, apps, webhooks and routing rules |
| Data source | `optimizelycmp_user` | by `email`; `optimizelycmp_users` lists everyone |
| Data source | `optimizelycmp_team`, `_workflow`, `_publishing_channel`, `_content_type`, `_field`, `_label_group`, `_brand_compliance_category` | by `name` |
| Data source | `optimizelycmp_template` | by `title` |

Most other CMP objects (campaigns, tasks, custom fields, content types) have no delete endpoint, so they are not
resources yet. See `examples/` for usage.

## Documentation

Step-by-step guides are in [docs/guides/](docs/guides/):

| Guide | Covers |
|---|---|
| [Getting started](docs/guides/getting-started.md) | install, credentials, a first read-only configuration |
| [Authentication and security](docs/guides/authentication.md) | how auth works, keeping the secret out of config |
| [Managing folders and access](docs/guides/managing-folders.md) | folders, permissions, ownership, lookups |
| [Structured content types](docs/guides/content-types.md) | fields as JSON, versions, destroy behaviour |
| [Custom fields](docs/guides/custom-fields.md) | field types, choices, destroy behaviour |
| [Reading settings](docs/guides/settings-export.md) | the settings export, secrets, what it leaves out |
| [Importing existing objects](docs/guides/importing-existing-objects.md) | `terraform import` and drift |
| [Troubleshooting](docs/guides/troubleshooting.md) | common errors and fixes |

## Configuration

| Attribute | Env var | Notes |
|---|---|---|
| `base_url` | `OPTIMIZELY_CMP_BASE_URL` | https only; default https://api.cmp.optimizely.com (`/v3` is added) |
| `token_url` | `OPTIMIZELY_CMP_TOKEN_URL` | default https://accounts.cmp.optimizely.com/o/oauth2/v1/token |
| `client_id` | `OPTIMIZELY_CMP_CLIENT_ID` | |
| `client_secret` | `OPTIMIZELY_CMP_CLIENT_SECRET` | sensitive; prefer the env var |

## Testing

| Command | What it does |
|---|---|
| `make test` | unit tests and the mock-API tests (`TestMock*`); no credentials needed. This is what CI runs (with the race detector). The mock tests run the real provider through the real Terraform CLI against `internal/mockcmp`, an in-memory fake of the CMP API, and are skipped if Terraform is not installed. |
| `make testacc` | acceptance tests against a **real CMP account** (use a sandbox). Needs `OPTIMIZELY_CMP_CLIENT_ID` and `OPTIMIZELY_CMP_CLIENT_SECRET` and Terraform on the PATH. Covers folders, folder permissions and the data sources, and cleans up after itself. |
| `make testacc-residue` | also runs the content type, custom field, label group, webhook, app and routing rule tests. The API cannot delete those, so **each run leaves a disabled content type, two inactive fields, a label group, a webhook, an app and a routing rule behind** (named `tfacc-...`) for an admin to remove by hand. |

Acceptance tests create everything with a random `tfacc-` name so reruns never collide. They are skipped unless `TF_ACC=1`
(the make targets set it), and they find the user and team they need by listing the account.

### The mock CMP

`internal/mockcmp` is a stateful fake of the API endpoints this provider uses, served over TLS. It also reproduces quirks found by
testing the real API: the settings import matching by name and never deleting, label groups created in the CMP interface being
known by a slug, defaults the API rewrites, templates and watchers it silently drops, and webhook events it rejects. When the real
API surprises the provider, teach the mock the same behaviour and add a `TestMock*` test, so the surprise stays covered in CI.

To publish a release, see [RELEASING.md](RELEASING.md).

## Development

```sh
make build   # build the provider binary
make test    # run unit tests
make docs    # regenerate docs/ (needs terraform on PATH)
make install # install into the local Terraform plugin mirror
```

To add a resource, put its API calls in `internal/client` and register the resource in `Resources()` in
`internal/provider/provider.go`.
