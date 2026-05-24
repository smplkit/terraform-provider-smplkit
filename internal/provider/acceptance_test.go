package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	smplkit "github.com/smplkit/go-sdk/v3"
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

// freeOneManagedEnvironmentSlot deletes the seeded `development`
// environment, if it exists, so the test below has a free
// platform.managed_environments slot to spend. A fresh free-tier
// account is born at 2/2 (production + development, both
// managed=true) per ADR-051 §3.4; production is system-protected and
// cannot be deleted, so development is the only candidate.
//
// Idempotent — a NotFoundError on delete is treated as "already
// gone", which lets a test rerun against a tenant that's already
// been pruned.
func freeOneManagedEnvironmentSlot(t *testing.T) {
	t.Helper()
	apiKey := os.Getenv("SMPLKIT_API_KEY")
	if apiKey == "" {
		t.Skip("SMPLKIT_API_KEY not set; skipping env-slot prep")
	}
	cfg := smplkit.ManagementConfig{APIKey: apiKey}
	if base := os.Getenv("SMPLKIT_API_URL"); base != "" {
		cfg.Scheme = "http"
		cfg.BaseDomain = "localhost"
	}
	client, err := smplkit.NewManagementClient(cfg)
	if err != nil {
		t.Fatalf("management client: %v", err)
	}
	if err := client.Environments().Delete(context.Background(), "development"); err != nil {
		var nf *smplkit.NotFoundError
		if !errors.As(err, &nf) {
			t.Fatalf("failed to free slot by deleting `development`: %v", err)
		}
	}
}

func TestAccEnvironmentResource_basic(t *testing.T) {
	id := fmt.Sprintf("tfacc-env-%d", time.Now().UnixNano())
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			freeOneManagedEnvironmentSlot(t)
		},
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

// TestAccConfigurationResource_sameRunEnvOverride is a regression test
// for a production-only bug surfaced by the smoke run: when a
// Terraform plan creates an environment and a configuration with a
// per-environment override referencing that environment in the same
// apply, the config service silently drops the override entry
// instead of either honoring it or returning 400 per ADR-051 §3.3.
// The framework then rejects the apply with "produced inconsistent
// result" because the response's `environments` map is null while
// the plan declared a non-null map.
//
// Gated on SMPLKIT_API_URL pointing at production. The bug doesn't
// reproduce against the local platform (everything on one
// docker-compose stack, no cross-service propagation delay), so
// running this test against the CI local platform would pass and
// give false confidence. CI sets SMPLKIT_API_URL=http://localhost
// and so skips this test, keeping main green; manual production
// runs exercise it.
//
// To run against production:
//
//	SMPLKIT_API_URL=https://app.smplkit.com \
//	SMPLKIT_API_KEY=sk_api_... \
//	TF_ACC=1 \
//	go test -run TestAccConfigurationResource_sameRunEnvOverride \
//	  ./internal/provider/... -v -count=1
//
// Until the server-side fix lands the test FAILS at apply. After the
// fix lands it should pass — the assertion on the round-tripped
// override value is the success signal.
func TestAccConfigurationResource_sameRunEnvOverride(t *testing.T) {
	if os.Getenv("SMPLKIT_API_URL") != "https://app.smplkit.com" {
		t.Skip("production-only regression test; set SMPLKIT_API_URL=https://app.smplkit.com to run")
	}

	envID := fmt.Sprintf("tfacc-bug-env-%d", time.Now().UnixNano())
	cfgID := fmt.Sprintf("tfacc-bug-cfg-%d", time.Now().UnixNano())

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_environment" "test" {
  id   = %[1]q
  name = "Bug Repro Env"
}

resource "smplkit_configuration" "test" {
  id   = %[2]q
  name = "Bug Repro Configuration"

  items = {
    cache_ttl_seconds = jsonencode(300)
  }

  # The same-run interaction under test: the override key references
  # smplkit_environment.test, which Terraform creates earlier in this
  # same apply. The server must either honor this override or return
  # 400 per ADR-051 §3.3 — the silent-drop path is the bug.
  environments = {
    (smplkit_environment.test.id) = {
      cache_ttl_seconds = jsonencode(60)
    }
  }
}
`, envID, cfgID),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Until the fix lands the apply never reaches
					// this Check — the framework fails the step on
					// "produced inconsistent result" first. After
					// the fix the apply succeeds and this assertion
					// is the actual regression signal: the override
					// must round-trip through the server with the
					// value we sent.
					resource.TestCheckResourceAttr(
						"smplkit_configuration.test",
						fmt.Sprintf("environments.%s.cache_ttl_seconds", envID),
						"60",
					),
				),
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
    headers = [
      {
        name  = "Authorization"
        value = "Splunk acc-test-token"
      },
      # Splunk HEC requires a Content-Type header — the audit service
      # validates this at the API edge for the splunk_hec type.
      {
        name  = "Content-Type"
        value = "application/json"
      },
    ]
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
