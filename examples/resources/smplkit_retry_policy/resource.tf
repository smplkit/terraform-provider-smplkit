resource "smplkit_retry_policy" "aggressive" {
  id   = "aggressive-retry"
  name = "Aggressive retry"

  # Up to 5 retries after the initial attempt, doubling the wait each time
  # (2s, 4s, 8s, …) but never waiting more than 60s.
  max_retries       = 5
  backoff           = "exponential"
  delay_seconds     = 2
  max_delay_seconds = 60

  # Retry only on the failures worth retrying: rate-limit and any server
  # error — but not a 501 Not Implemented, which won't change on retry —
  # plus connection failures and timeouts. Status patterns are exact codes
  # or classes (1xx–5xx).
  retry_statuses            = ["429", "5xx"]
  retry_statuses_except     = ["501"]
  retry_on_connection_error = true
  retry_on_timeout          = true
}

# Reference the policy from a job's base retry_policy, overridable per
# environment via the environments map.
resource "smplkit_job" "nightly_cache_warm" {
  id           = "nightly-cache-warm"
  name         = "Nightly cache warm"
  schedule     = "0 2 * * *"
  retry_policy = smplkit_retry_policy.aggressive.id

  configuration = {
    url = "https://api.example.com/cache/warm"
  }
}
