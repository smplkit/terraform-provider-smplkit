data "smplkit_job" "nightly_cache_warm" {
  id = "nightly-cache-warm"
}

output "next_run_at" {
  value = data.smplkit_job.nightly_cache_warm.next_run_at
}
