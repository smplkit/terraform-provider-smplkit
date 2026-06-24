variable "cache_warm_token" {
  type      = string
  sensitive = true
}

resource "smplkit_retry_policy" "cache_warm_retry" {
  id            = "cache-warm-retry"
  name          = "Cache warm retry"
  max_retries               = 3
  backoff                   = "exponential"
  delay_seconds             = 5
  retry_on_connection_error = true
  retry_on_timeout          = true
}

resource "smplkit_job" "nightly_cache_warm" {
  id          = "nightly-cache-warm"
  name        = "Nightly cache warm"
  description = "Warms the product cache every night at 02:00 UTC."

  # A 5-field cron expression. Use an ISO-8601 datetime for a one-off
  # run, or "now" to run once as soon as possible.
  schedule = "0 2 * * *"

  # IANA timezone the cron is evaluated in (recurring jobs only). Omit
  # for UTC.
  timezone = "America/New_York"

  # Base retry policy for failed runs — the id of a smplkit_retry_policy,
  # overridable per environment via the environments map.
  retry_policy = smplkit_retry_policy.cache_warm_retry.id

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
