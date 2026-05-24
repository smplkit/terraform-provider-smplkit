resource "smplkit_configuration" "app_settings" {
  id          = "app_settings"
  name        = "App Settings"
  description = "Per-environment runtime configuration for the app service."

  # Use jsonencode(...) for every value — items are JSON-encoded strings on
  # the Terraform side so any JSON-serializable value (string, number, bool,
  # array, object) round-trips faithfully.
  items = {
    debug             = jsonencode(false)
    timeout_ms        = jsonencode(5000)
    region            = jsonencode("us-west-2")
    feature_flags     = jsonencode({ alpha = true, beta = false })
    allowed_hostnames = jsonencode(["api.example.com", "app.example.com"])
  }

  # Per-environment overrides apply on top of the base items.
  environments = {
    production = {
      debug      = jsonencode(false)
      timeout_ms = jsonencode(2500)
    }
    staging = {
      debug = jsonencode(true)
    }
  }
}
