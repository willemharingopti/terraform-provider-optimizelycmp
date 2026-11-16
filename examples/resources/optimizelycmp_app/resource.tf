resource "optimizelycmp_app" "portal" {
  name         = "Content portal"
  description  = "Our internal content portal"
  homepage_url = "https://portal.example.com"
  expose_email = false

  # URLs can be added but never removed.
  authorization_callback_urls = ["https://portal.example.com/auth/callback"]
}
