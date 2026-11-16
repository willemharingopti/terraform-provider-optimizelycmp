data "optimizelycmp_team" "marketing" {
  name = "Marketing"
}

resource "optimizelycmp_folder_permission" "marketing_edit" {
  folder_id     = optimizelycmp_folder.brand.id
  accessor_id   = data.optimizelycmp_team.marketing.id
  accessor_type = "team"
  access_type   = "edit"
}
