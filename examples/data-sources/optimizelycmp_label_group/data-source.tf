data "optimizelycmp_label_group" "pillar" {
  name = "Content Pillar"
}

output "pillar_values" {
  value = data.optimizelycmp_label_group.pillar.value_names
}
