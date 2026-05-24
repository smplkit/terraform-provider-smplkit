variable "splunk_hec_token" {
  type      = string
  sensitive = true
}

resource "smplkit_audit_forwarder" "splunk_prod" {
  id             = "splunk-prod"
  name           = "Splunk HEC — Production"
  description    = "Forwards audit events to the production Splunk instance."
  forwarder_type = "splunk_hec"
  enabled        = true

  # Optional JSON Logic filter — only forward events about flag changes.
  filter = jsonencode({
    "in" : [{ "var" : "resource_type" }, ["smpl.flag", "flag"]]
  })

  # Optional JSONATA template applied to each event before delivery.
  transform_type = "JSONATA"
  transform      = "{ \"timestamp\": occurred_at, \"actor\": actor_label, \"event\": event_type }"

  configuration = {
    url            = "https://splunk.example.com:8088/services/collector/event"
    method         = "POST"
    success_status = "200"
    headers = [
      {
        name  = "Authorization"
        value = "Splunk ${var.splunk_hec_token}"
      },
    ]
  }
}
