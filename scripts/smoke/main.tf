# Production smoke test — exercises every resource type the provider ships.
# Triggered by the release workflow against an ephemeral account so the
# resources land in a freshly-provisioned tenant and are removed at the
# end via terraform destroy + account delete.

terraform {
  required_providers {
    smplkit = {
      source  = "smplkit/smplkit"
      version = ">= 0.1.0"
    }
  }
}

provider "smplkit" {
  # SMPLKIT_API_KEY is exported by the bootstrap step.
}

variable "smoke_nonce" {
  type        = string
  description = "Unique suffix appended to every resource id so reruns don't collide."
}

resource "smplkit_service" "smoke" {
  id   = "smoke-service-${var.smoke_nonce}"
  name = "Smoke Service"
}

resource "smplkit_environment" "smoke" {
  id             = "smoke-env-${var.smoke_nonce}"
  name           = "Smoke Env"
  color          = "#10b981"
  classification = "STANDARD"
}

resource "smplkit_configuration" "smoke" {
  id          = "smoke-config-${var.smoke_nonce}"
  name        = "Smoke Configuration"
  description = "Configuration applied by the Terraform provider release smoke test."

  items = {
    cache_ttl_seconds = jsonencode(300)
    region            = jsonencode("us-west-2")
    feature_x         = jsonencode(true)
  }

  environments = {
    (smplkit_environment.smoke.id) = {
      cache_ttl_seconds = jsonencode(60)
    }
  }
}

resource "smplkit_flag" "smoke" {
  id      = "smoke-flag-${var.smoke_nonce}"
  name    = "Smoke Flag"
  type    = "BOOLEAN"
  default = jsonencode(false)

  environments = {
    (smplkit_environment.smoke.id) = {
      enabled = true
      default = jsonencode(true)
      rules = [{
        description = "Always enable for the smoke evaluator"
        logic       = jsonencode({ "==" : [{ "var" : "user.smoke" }, true] })
        value       = jsonencode(true)
      }]
    }
  }
}

resource "smplkit_log_group" "smoke" {
  id    = "smoke-log-group-${var.smoke_nonce}"
  name  = "Smoke Log Group"
  level = "INFO"

  environments = {
    (smplkit_environment.smoke.id) = "DEBUG"
  }
}

resource "smplkit_audit_forwarder" "smoke" {
  id             = "smoke-forwarder-${var.smoke_nonce}"
  name           = "Smoke Forwarder"
  forwarder_type = "http"
  enabled        = false # never actually deliver during the smoke run

  configuration = {
    url    = "https://example.com/audit-events"
    method = "POST"
    headers = [
      { name = "X-Smoke-Run", value = var.smoke_nonce },
    ]
  }
}
