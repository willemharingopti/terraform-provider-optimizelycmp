resource "optimizelycmp_folder" "brand" {
  name = "Brand"
}

resource "optimizelycmp_folder" "logos" {
  name             = "Logos"
  parent_folder_id = optimizelycmp_folder.brand.id
}
