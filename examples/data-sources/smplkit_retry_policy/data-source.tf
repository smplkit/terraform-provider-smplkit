data "smplkit_retry_policy" "aggressive" {
  id = "aggressive-retry"
}

output "aggressive_max_retries" {
  value = data.smplkit_retry_policy.aggressive.max_retries
}
