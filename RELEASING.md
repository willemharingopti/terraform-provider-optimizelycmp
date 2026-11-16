# Releasing to the Terraform Registry

The registry reads releases from GitHub. A release is a **signed**, semantically versioned tag whose assets are built by
GoReleaser (`.goreleaser.yml`) from the workflow in `.github/workflows/release.yml`.

This provider talks to experimental parts of the CMP API (custom fields, content types and the settings import are all marked
experimental by CMP). Say so in the release notes, and expect minor versions below 1.0 to contain breaking changes.

## One-time setup

1. **Repository.** Create a public GitHub repository named `terraform-provider-optimizelycmp`. The registry address is
   `willemharingopti/optimizelycmp`, where `willemharingopti` is the GitHub user (or organisation) that owns the repository.
   Then connect this checkout to it and push the first commit:

   ```sh
   git remote add terraform-provider-optimizelycmp https://github.com/willemharingopti/terraform-provider-optimizelycmp.git
   git push terraform-provider-optimizelycmp main
   ```

   (The remote carries the repository's name, as in cms-terraform, so a push never goes to the wrong provider by accident.)
2. **Module path.** `go.mod` and the imports use `github.com/willemharingopti/terraform-provider-optimizelycmp`, and the registry
   address appears in `main.go`, the examples, the docs and the guides. If you publish under another owner, change all of
   them together: `grep -r willemharingopti .` finds them.
3. **GPG signing key.** Releases are signed so Terraform can verify them.

   ```sh
   gpg --full-generate-key                      # choose RSA, 4096 bits; use a strong passphrase
   gpg --list-secret-keys --keyid-format=long   # note the key ID
   gpg --armor --export <KEY ID> > public.asc   # the PUBLIC key, for the registry
   gpg --armor --export-secret-keys <KEY ID>    # the PRIVATE key, for the GitHub secret below
   ```

   Keep the private key and passphrase out of the repository, shell history and chat. Store a backup in your password
   manager, and delete any exported private-key file once it is saved as a secret. `.gitignore` already excludes `*.asc` and
   `*.gpg` so a key can never be committed by accident.
4. **GitHub secrets and environment.** In the repository settings, create an environment named `release`, add a required
   reviewer to it, and add two secrets *to that environment*: `GPG_PRIVATE_KEY` (the armored private key) and `PASSPHRASE`.
   `GITHUB_TOKEN` is supplied automatically. Also protect `v*` tags with a ruleset, so only maintainers can create them.
5. **Registry.** Sign in at <https://registry.terraform.io> with GitHub, open *User Settings > Signing Keys* and add the
   **public** key for your namespace. Then choose *Publish > Provider*, and select the repository. The registry installs a
   webhook that notices new releases.
6. **Keep credentials out of the repository.** `.env` (the CMP API credentials used for local testing) is git-ignored. Never
   commit it, and never add credentials to workflows: CI runs only the tests that need no account.

## Making a release

1. Make sure you are on a clean, up-to-date `main`.
2. In `CHANGELOG.md`, rename `## Unreleased` to the new version and date, and start a new empty `## Unreleased` above it.
3. Check:

   ```sh
   make test          # unit tests and the mock-API tests (needs Terraform on the PATH for the mock-API tests)
   make docs          # must leave no changes in docs/ (CI fails if it does)
   git status         # clean, apart from the changelog edit
   ```

   Before the first release, and after any change that touches how the provider talks to CMP, also run `make testacc` against a
   **sandbox** account. It cleans up after itself. `make testacc-residue` leaves objects behind that CMP cannot delete, so use it
   sparingly (see the README).
4. Optionally rehearse the build, which signs nothing and publishes nothing:

   ```sh
   make check         # validates .goreleaser.yml
   make snapshot      # builds every platform into dist/
   ```

   Both download and run GoReleaser, pinned to the version in `GORELEASER` in `GNUmakefile` (currently v2.20.2). The release
   workflow uses `goreleaser-action`, which has its own version (`~> v2`, see `release.yml`), so when you upgrade one, check the
   other.
5. Commit the changelog, then tag and push. The version must be `v` followed by a semantic version:

   ```sh
   git commit -am "Release v0.1.0"
   git tag v0.1.0
   git push terraform-provider-optimizelycmp main v0.1.0
   ```

6. Approve the `release` environment when the workflow asks. It builds the zips, writes `..._SHA256SUMS` and its
   `.sig`, attaches `..._manifest.json`, and creates the GitHub release.
7. The registry picks the release up within minutes. A version, once ingested, cannot be replaced: if something is wrong,
   publish the next version (for example `v0.1.1`) instead of re-tagging.

## Checking a release

After the registry shows the version, in an empty directory:

```sh
cat > main.tf <<'EOT'
terraform {
  required_providers {
    optimizelycmp = { source = "willemharingopti/optimizelycmp", version = "0.1.0" }
  }
}
EOT
terraform init
```

`terraform init` downloads the provider, checks its signature, and writes `.terraform.lock.hcl`.

## Documentation

`docs/` is what the registry displays. The reference pages are generated (`make docs`); edit the schema descriptions,
`examples/` or `templates/guides/` and regenerate, never the generated files. CI fails when `docs/` is out of date.
