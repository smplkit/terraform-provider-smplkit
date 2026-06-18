data "smplkit_job" "nightly_cache_warm" {
  id = "nightly-cache-warm"
}

# next_run_at is now per-environment: each entry in the environments map
# carries the next scheduled fire time for that environment.
output "production_next_run_at" {
  value = data.smplkit_job.nightly_cache_warm.environments["production"].next_run_at
}
