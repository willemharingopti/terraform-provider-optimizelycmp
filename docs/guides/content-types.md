---
page_title: "Structured content types"
subcategory: ""
description: |-
  Define structured content types and their fields, and how versions and destroy behave.
---

# Structured content types

`optimizelycmp_content_type` manages a CMP structured content type: its details (name, description, whether it is a
component) and its fields. The CMP API marks these endpoints as **experimental**, so expect them to change.

## Example

```terraform
data "optimizelycmp_user" "me" {
  email = "jane.doe@example.com"
}

resource "optimizelycmp_content_type" "article" {
  name           = "Article"
  component      = false
  acting_user_id = data.optimizelycmp_user.me.id

  field_definitions_json = jsonencode([
    {
      core = {
        key                       = "headline"
        name                      = "Headline"
        field_type                = "text-field"
        is_list                   = false
        is_required               = true
        need_internationalization = true
      }
      validation_pattern = ""
      min_length         = 1
    },
  ])
}
```

## Fields are JSON

CMP has eight kinds of field definition (text, rich text, date and time, number, choice, library asset, content-type
reference, and a base kind for boolean, json, url and location). Rather than model each one, the resource takes
`field_definitions_json`, a JSON array in exactly the shape the API documents. Build it with `jsonencode()`.

Each field needs a `core` block (`key`, `name`, `is_list`, `is_required`, `need_internationalization`, and usually
`field_type`) plus the properties of its kind. The kinds and their required properties are listed in the
[CMP API reference](https://docs.optimizely.com/content-marketing-platform/api) under *Create content type*.

The API normalises field definitions: for example an empty `validation_pattern` is stored as `".*"`. To avoid a
permanent difference, the provider does not compare the JSON itself. It remembers the ID of the version it last
published (`latest_version_id`). If the latest version in CMP is still that one, your JSON is taken as applied. If someone
publishes a new version in CMP, the next `terraform plan` shows your JSON as changed and offers to publish yours over it.

## Versions

Fields live in versions. Changing `field_definitions_json` or `expected_locales` publishes a **new version**. Changing the
name, description, `component`, `disabled` or `thumbnail_guid` updates the content type itself and publishes no version.
`latest_version_id` shows the current version.

## The acting user

The API records who created and updated a content type and requires a user ID for it. Set `acting_user_id` with the
`optimizelycmp_user` data source. It is not read back, and changing it alone makes no API call.

## Destroying

The API has **no delete** for content types. Destroying the resource **disables** the content type
(`disabled = true`) and removes it from state, with a warning. It stays in CMP, so a later resource with the same name
creates a second content type. To bring a disabled one back under management, import it and set `disabled = false`.

## Importing

```sh
terraform import optimizelycmp_content_type.article <content_type_guid>
```

Set `acting_user_id` and a `field_definitions_json` matching the type, then run `terraform plan` to see what differs.

## Looking one up

```terraform
data "optimizelycmp_content_type" "article" {
  name = "Article"
}
```

returns `id`, `component` and `disabled`. It fails if no content type, or more than one, has that name.
