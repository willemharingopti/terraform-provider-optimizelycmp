resource "optimizelycmp_routing_rule" "legal" {
  name          = "Legal review requests"
  description   = "Send requests tagged for legal to the legal template"
  template_name = "Article Post"
  watchers      = ["legal@example.com"]

  rules = [
    {
      field_type = "label"
      identifier = "content_pillar"
      operator   = "equal"
      value      = ["Brand"]
    },
  ]
}
