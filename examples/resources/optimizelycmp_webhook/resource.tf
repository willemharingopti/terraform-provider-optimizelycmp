variable "webhook_secret" {
  type      = string
  sensitive = true
}

resource "optimizelycmp_webhook" "tasks" {
  name         = "Task events"
  description  = "Sends task events to our service"
  callback_url = "https://hooks.example.com/cmp"
  secret       = var.webhook_secret

  # Events can be added but never removed.
  event_names = ["task_added", "task_modified", "task_removed"]
}
