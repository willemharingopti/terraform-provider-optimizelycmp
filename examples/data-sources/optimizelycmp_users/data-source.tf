data "optimizelycmp_users" "all" {}

# Everyone with the Creator role.
output "creators" {
  value = [for u in data.optimizelycmp_users.all.users : u.full_name if contains(u.roles, "Creator")]
}
