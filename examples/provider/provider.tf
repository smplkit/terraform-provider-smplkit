terraform {
  required_providers {
    smplkit = {
      source  = "smplkit/smplkit"
      version = "~> 1.0"
    }
  }
}

# The provider reads SMPLKIT_API_KEY from the environment by default.
# Set api_url to talk to the local platform (ADR-042) instead of production.
provider "smplkit" {
  # api_key = "sk_api_..."          # or set SMPLKIT_API_KEY in the environment
  # api_url = "http://localhost"    # local platform via the Caddy proxy
}

# A minimal first example — declare a service and a configuration that
# inherits the service's id as its key. Both id and name are required.
resource "smplkit_service" "user_service" {
  id   = "user_service"
  name = "User Service"
}

resource "smplkit_configuration" "user_service_config" {
  id   = smplkit_service.user_service.id
  name = "User Service Config"

  items = {
    cache_ttl_seconds = jsonencode(300)
    feature_x         = jsonencode(true)
  }
}
