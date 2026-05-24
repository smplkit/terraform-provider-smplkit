package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Acceptance tests run against a real smplkit platform (the local
// platform per ADR-042 by default; production via the release smoke
// job). They're guarded by TF_ACC=1, which is the framework's standard
// gate, and they only run when the environment is configured —
// SMPLKIT_API_KEY must be set (and SMPLKIT_API_URL when targeting the
// local platform).

const localPlatformURL = "http://localhost"

// testAccProtoV6ProviderFactories wires our provider into the testing
// framework's harness. The factory is invoked once per test; the
// underlying ManagementClient picks up SMPLKIT_API_KEY from the env.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"smplkit": providerserver.NewProtocol6WithError(New("acc")()),
}

func testAccPreCheck(t *testing.T) {
	if os.Getenv("SMPLKIT_API_KEY") == "" {
		t.Skip("SMPLKIT_API_KEY not set; skipping acceptance test")
	}
	if os.Getenv("SMPLKIT_API_URL") == "" {
		t.Setenv("SMPLKIT_API_URL", localPlatformURL)
	}
}

// ─── smplkit_service ───────────────────────────────────────────────────────

func TestAccServiceResource_basic(t *testing.T) {
	id := fmt.Sprintf("tfacc-svc-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_service" "test" {
  id   = %[1]q
  name = "Acc Service"
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_service.test", "id", id),
					resource.TestCheckResourceAttr("smplkit_service.test", "name", "Acc Service"),
					resource.TestCheckResourceAttrSet("smplkit_service.test", "created_at"),
				),
			},
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_service" "test" {
  id   = %[1]q
  name = "Acc Service (renamed)"
}
`, id),
				Check: resource.TestCheckResourceAttr("smplkit_service.test", "name", "Acc Service (renamed)"),
			},
			{
				ResourceName:      "smplkit_service.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ─── smplkit_environment ───────────────────────────────────────────────────

func TestAccEnvironmentResource_basic(t *testing.T) {
	id := fmt.Sprintf("tfacc-env-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_environment" "test" {
  id    = %[1]q
  name  = "Acc Env"
  color = "#10b981"
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_environment.test", "id", id),
					resource.TestCheckResourceAttr("smplkit_environment.test", "color", "#10b981"),
					resource.TestCheckResourceAttr("smplkit_environment.test", "classification", "STANDARD"),
				),
			},
			{
				ResourceName:      "smplkit_environment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ─── smplkit_configuration ─────────────────────────────────────────────────

func TestAccConfigurationResource_basicAndUpdate(t *testing.T) {
	id := fmt.Sprintf("tfacc-cfg-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_configuration" "test" {
  id          = %[1]q
  name        = "Acc Config"
  description = "Initial"
  items = {
    a = jsonencode(1)
    b = jsonencode("two")
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_configuration.test", "items.a", "1"),
					resource.TestCheckResourceAttr("smplkit_configuration.test", "items.b", `"two"`),
				),
			},
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_configuration" "test" {
  id          = %[1]q
  name        = "Acc Config"
  description = "Updated"
  items = {
    a = jsonencode(99)
    c = jsonencode(true)
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_configuration.test", "description", "Updated"),
					resource.TestCheckResourceAttr("smplkit_configuration.test", "items.a", "99"),
					resource.TestCheckResourceAttr("smplkit_configuration.test", "items.c", "true"),
					resource.TestCheckNoResourceAttr("smplkit_configuration.test", "items.b"),
				),
			},
			{
				ResourceName:      "smplkit_configuration.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ─── smplkit_flag ──────────────────────────────────────────────────────────

func TestAccFlagResource_withRulesAndOverrides(t *testing.T) {
	id := fmt.Sprintf("tfacc-flag-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_flag" "test" {
  id      = %[1]q
  name    = "Acc Flag"
  type    = "BOOLEAN"
  default = jsonencode(false)

  environments = {
    production = {
      enabled = true
      default = jsonencode(false)
      rules = [{
        description = "Enable for enterprise plan"
        logic       = jsonencode({ "==" : [{ "var" : "user.plan" }, "enterprise"] })
        value       = jsonencode(true)
      }]
    }
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_flag.test", "type", "BOOLEAN"),
					resource.TestCheckResourceAttr("smplkit_flag.test", "default", "false"),
					resource.TestCheckResourceAttr("smplkit_flag.test", "environments.production.enabled", "true"),
					resource.TestCheckResourceAttr("smplkit_flag.test", "environments.production.rules.#", "1"),
				),
			},
			{
				ResourceName:      "smplkit_flag.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ─── smplkit_log_group ─────────────────────────────────────────────────────

func TestAccLogGroupResource_basic(t *testing.T) {
	id := fmt.Sprintf("tfacc-lg-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_log_group" "test" {
  id    = %[1]q
  name  = "Acc Log Group"
  level = "INFO"

  environments = {
    production = "WARN"
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_log_group.test", "level", "INFO"),
					resource.TestCheckResourceAttr("smplkit_log_group.test", "environments.production", "WARN"),
				),
			},
			{
				ResourceName:      "smplkit_log_group.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

// ─── smplkit_audit_forwarder ───────────────────────────────────────────────

func TestAccAuditForwarderResource_typeAwareConfig(t *testing.T) {
	id := fmt.Sprintf("tfacc-fwd-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_audit_forwarder" "test" {
  id             = %[1]q
  name           = "Acc Forwarder"
  forwarder_type = "splunk_hec"
  enabled        = false

  configuration = {
    url = "https://splunk.example.com:8088/services/collector/event"
    headers = [{
      name  = "Authorization"
      value = "Splunk acc-test-token"
    }]
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "forwarder_type", "splunk_hec"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "enabled", "false"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "configuration.url", "https://splunk.example.com:8088/services/collector/event"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "configuration.headers.0.name", "Authorization"),
				),
			},
			{
				ResourceName:            "smplkit_audit_forwarder.test",
				ImportState:             true,
				ImportStateVerify:       true,
				// header values come back redacted, so the import-verify
				// pass would always diff against the plan value
				ImportStateVerifyIgnore: []string{"configuration.headers"},
			},
		},
	})
}

// testProviderConfig returns a small Terraform block that pins the
// provider to whatever SMPLKIT_API_URL the test harness picked. The
// acceptance harness passes SMPLKIT_API_KEY through the environment, so
// the provider block reads everything from env vars.
func testProviderConfig() string {
	apiURL := os.Getenv("SMPLKIT_API_URL")
	if apiURL == "" {
		apiURL = localPlatformURL
	}
	return fmt.Sprintf(`
provider "smplkit" {
  api_url = %q
}
`, apiURL)
}
