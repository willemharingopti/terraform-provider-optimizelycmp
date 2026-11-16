resource "optimizelycmp_field" "priority" {
  name        = "Priority"
  type        = "dropdown"
  helper_text = "How urgent is this?"

  # The keys (low, high) are for Terraform only: they let you rename a choice without recreating it.
  choices = {
    low  = { name = "Low" }
    high = { name = "High" }
  }
}

resource "optimizelycmp_field" "budget" {
  name                   = "Budget"
  type                   = "currency_number"
  currency_code          = "EUR"
  decimal_places         = 2
  has_thousand_separator = true
}
