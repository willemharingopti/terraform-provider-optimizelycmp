data "optimizelycmp_user" "me" {
  email = "jane.doe@example.com"
}

resource "optimizelycmp_content_type" "article" {
  name           = "Article"
  description    = "A news article"
  component      = false
  acting_user_id = data.optimizelycmp_user.me.id

  expected_locales = ["en"]

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
      max_length         = 120
    },
    {
      core = {
        key                       = "featured"
        name                      = "Featured"
        field_type                = "boolean"
        is_list                   = false
        is_required               = false
        need_internationalization = false
      }
      base_type = "boolean"
    },
  ])
}
