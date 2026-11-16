data "optimizelycmp_settings" "current" {}

# Each section is JSON text in the API's own shape.
locals {
  workflow_names = [for w in jsondecode(data.optimizelycmp_settings.current.workflows_json) : w.name]
  label_groups   = [for l in jsondecode(data.optimizelycmp_settings.current.labels_json) : l.label_type]
}

# webhooks_json is sensitive because it contains each webhook's secret.
output "webhook_names" {
  value = [for w in jsondecode(nonsensitive(data.optimizelycmp_settings.current.webhooks_json)) : w.name]
}
