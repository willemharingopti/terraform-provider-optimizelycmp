---
page_title: "Custom fields"
subcategory: ""
description: |-
  Define organisation-level custom fields and their choices, and how destroy behaves.
---

# Custom fields

`optimizelycmp_field` manages an organisation-level custom field, the kind you attach to tasks, campaigns and workflows.
The CMP API marks these endpoints as **experimental**.

## Types

`type` is one of `text`, `text_area`, `rich_text`, `date`, `image`, `video`, `simple_number`, `percentage_number`,
`currency_number`, `dropdown`, `label`, `checkbox`, `radio_button`. **The type cannot be changed**: changing it replaces the
field, and because fields cannot be deleted the old one is left behind, deactivated.

## Simple fields

```terraform
resource "optimizelycmp_field" "budget" {
  name                   = "Budget"
  type                   = "currency_number"
  currency_code          = "EUR"
  decimal_places         = 2
  has_thousand_separator = true
}
```

`decimal_places` and `has_thousand_separator` apply to the number types, and `currency_code` to `currency_number`. Leave a
setting out to take the API's default (2 decimal places, USD).

## Fields with choices

`dropdown`, `label`, `checkbox` and `radio_button` fields need at least one choice. Other types cannot have any.

```terraform
resource "optimizelycmp_field" "priority" {
  name = "Priority"
  type = "dropdown"

  choices = {
    low  = { name = "Low" }
    high = { name = "High" }
  }
}
```

The keys (`low`, `high`) are only for Terraform. They let it recognise a choice when you rename it, so a rename updates the
choice in place. Choice names must be unique within a field.

- `is_multi_select` (dropdown and label only) allows several values. It defaults to `false`.
- `color` (label only) is one of `#4ECFD5`, `#FFC700`, `#FF98A7`, `#702BD5`, `#5FEEAD`, `#D6C4F2`, `#5F9FF6`, `#CB5DEB`, `#9694B3`.
- `is_active = false` on a choice keeps it on the field but stops users picking it.
- Removing a key deletes the choice. The API refuses to delete a field's **last active** choice, so to empty a field, deactivate
  the field instead.
- Choices someone adds in CMP outside Terraform are ignored; Terraform only tracks the ones in your configuration.

## Destroying

The API has **no delete** for fields. Destroying the resource **deactivates** the field (`is_active = false`) and removes it from
state, with a warning. Inactive fields cannot be used in tasks, campaigns or workflows, but they remain in CMP.

## Importing

```sh
terraform import optimizelycmp_field.priority <field_id>
```

Import reads the field but does not track its choices, because they need keys. After importing, add a `choices` block whose
names match the existing ones; Terraform then plans changes to them, so check the plan before applying.

## Looking one up

```terraform
data "optimizelycmp_field" "priority" {
  name = "Priority"
}
```

returns `id`, `type`, `is_active` and `helper_text`. It fails if no field, or more than one, has that name.
