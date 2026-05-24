# A boolean flag with per-environment overrides and a targeting rule.
resource "smplkit_flag" "dark_mode" {
  id          = "dark_mode"
  name        = "Dark Mode"
  type        = "BOOLEAN"
  description = "Enables the dark theme in the user-facing console."
  default     = jsonencode(false)

  environments = {
    production = {
      enabled = true
      default = jsonencode(false)
      rules = [
        {
          description = "Enterprise tier opt-in"
          logic       = jsonencode({ "==" : [{ "var" : "user.plan" }, "enterprise"] })
          value       = jsonencode(true)
        },
      ]
    }
    staging = {
      enabled = true
      default = jsonencode(true)
    }
  }
}

# A string flag with a constrained value set — the console renders these
# as a dropdown.
resource "smplkit_flag" "release_channel" {
  id      = "release_channel"
  name    = "Release Channel"
  type    = "STRING"
  default = jsonencode("stable")

  values = [
    { name = "Stable", value = jsonencode("stable") },
    { name = "Beta", value = jsonencode("beta") },
    { name = "Alpha", value = jsonencode("alpha") },
  ]
}
