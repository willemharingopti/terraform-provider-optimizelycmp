---
page_title: "Authentication and security"
subcategory: ""
description: |-
  How the provider authenticates, and how to keep the client secret out of configuration and state.
---

# Authentication and security

## How it authenticates

The provider asks the CMP authorization server for an access token using the OAuth 2.0 **client-credentials** flow, then
sends it as a bearer token on every API call. The token is held in memory only, refreshed shortly before it expires, and
refreshed once more if the API answers `401`. It is never written to state.

Because the flow has no user in it, the provider acts as the API client, not as a person. What it can see and change is
what that client is allowed to.

## Settings

| Setting | Environment variable | Default |
|---|---|---|
| `client_id` | `OPTIMIZELY_CMP_CLIENT_ID` | none, required |
| `client_secret` | `OPTIMIZELY_CMP_CLIENT_SECRET` | none, required, sensitive |
| `base_url` | `OPTIMIZELY_CMP_BASE_URL` | `https://api.cmp.optimizely.com` |
| `token_url` | `OPTIMIZELY_CMP_TOKEN_URL` | `https://accounts.cmp.optimizely.com/o/oauth2/v1/token` |

A value in the provider block wins over the environment variable. `base_url` and `token_url` must be `https`. A trailing
`/v3` on `base_url` is accepted; the provider adds it if missing.

## Keep the secret out of your configuration

Prefer the environment variables. A `client_secret` written in the provider block sits in plain text in your
configuration and in version control if you commit it.

- **Terraform does not read `.env` files.** If you keep credentials in one, load it into your shell first:
  `set -a; source .env; set +a`.
- **Never commit** a file holding the secret. Add it to `.gitignore`.
- In CI, store the values as the pipeline's secrets and expose them as environment variables for the Terraform step only.
- Use a dedicated API client for Terraform so you can rotate or revoke it without affecting other integrations.

## State

The folders and permissions Terraform manages are stored in state with their IDs, names and access levels, but not
credentials or tokens. Treat state as internal data anyway, and store it in a backend with access control.

## If a secret leaks

Revoke or regenerate the API client's secret in CMP, update your environment or pipeline secret, and run
`terraform plan` to confirm the new secret works.
