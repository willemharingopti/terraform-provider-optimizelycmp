# Changelog

## Unreleased

- Initial provider: folder and folder permission resources; user, team, workflow, template and publishing channel data sources.
- `optimizelycmp_content_type` resource and data source (structured content types; destroy disables).
- `optimizelycmp_field` resource and data source (custom fields and their choices; destroy deactivates).
- Data sources: `optimizelycmp_users`, `optimizelycmp_label_group`, `optimizelycmp_brand_compliance_category`; team, workflow and template lookups now return members, flags, steps and applicability.
- Acceptance test suite (`make testacc`, `make testacc-residue`).
- `optimizelycmp_settings` data source (read-only export of the organisation's configuration).
- `optimizelycmp_label_group` resource (written through the bulk settings import; labels can only be added).
- `optimizelycmp_webhook`, `optimizelycmp_app` and `optimizelycmp_routing_rule` resources (written through the bulk settings import; nothing can be deleted).
- Hardening: a 302 is only accepted for the user lookup, oversized responses and endless pagination are refused, and unknown provider settings are rejected instead of read as unset.
- Settings export is cached briefly and cleared by every write, so refreshing many settings-based resources downloads it once.
- `internal/mockcmp` and `TestMock*` tests: the resources are now tested end to end in CI without a CMP account.
