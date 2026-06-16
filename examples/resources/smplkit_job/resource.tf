variable "cache_warm_token" {
  type      = string
  sensitive = true
}

resource "smplkit_job" "nightly_cache_warm" {
  id          = "nightly-cache-warm"
  name        = "Nightly cache warm"
  description = "Warms the product cache every night at 02:00 UTC."

  # A 5-field cron expression (UTC). Use an ISO-8601 datetime for a
  # one-off run, or "now" to run once as soon as possible.
  schedule = "0 2 * * *"

  configuration = {
    url            = "https://api.example.com/cache/warm"
    method         = "POST"
    body           = jsonencode({ scope = "all" })
    success_status = "2xx"
    timeout        = 30

    headers = [
      {
        name  = "Authorization"
        value = "Bearer ${var.cache_warm_token}"
      },
      {
        name  = "Content-Type"
        value = "application/json"
      },
    ]
  }
}
