resource "smplkit_log_group" "http" {
  id    = "http"
  name  = "HTTP Handlers"
  level = "INFO"

  environments = {
    production = "WARN"
    staging    = "DEBUG"
  }
}

# Nested groups: this group inherits from its parent unless it sets its own level.
resource "smplkit_log_group" "http_routes" {
  id           = "http_routes"
  name         = "HTTP Routes"
  parent_group = smplkit_log_group.http.id
}
