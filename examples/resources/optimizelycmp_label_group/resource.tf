resource "optimizelycmp_label_group" "pillar" {
  name             = "Content Pillar"
  has_single_value = false

  # Keyed by label name. Labels can be added and recoloured, never removed.
  labels = {
    Brand     = { color = "#4ECFD5" }
    Product   = { color = "#FFC700" }
    Community = { color = "#FF98A7" }
  }
}
