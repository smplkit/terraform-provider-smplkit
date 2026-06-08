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
	// Route this side-channel client at the same backend the provider
	// under test targets. SMPLKIT_API_URL may point at the local platform
	// (`http://localhost`) or at a remote/prod endpoint
	// (`https://app.smplkit.com`); parse it through the same splitAPIURL
	// the provider uses so prod runs don't get silently pinned to
	// localhost (which yields a spurious 502/connection error here).
	if raw := os.Getenv("SMPLKIT_API_URL"); raw != "" {
		scheme, base, err := splitAPIURL(raw)
		if err != nil {
			t.Fatalf("invalid SMPLKIT_API_URL %q: %v", raw, err)
		}
		cfg.Scheme = scheme
		cfg.BaseDomain = base
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
// for a bug that landed in v1.0.0: a Terraform plan that creates an
// environment and a sibling configuration referencing that environment
// in a per-environment override silently dropped the override on the
// wire. The root cause was a hand-written wrapper in go-sdk that
// turned the flat per-env shape into an empty envelope when serializing
// to the old `{values: {key: {value: V}}}` request body; the response
// then came back with environments=null and the framework rejected the
// apply with "produced inconsistent result."
//
// The fix landed across the stack: ADR-024 §2.4 was revised to use the
// flat `{env: {key: V}}` shape on the wire, the config service and all
// 6 SDKs were updated, and this test guards the same-plan-env-override
// case so the bug can't return silently.
//
// Runs against whatever backend SMPLKIT_API_URL points at — CI's
// acceptance workflow exercises it against the local platform; the
// release smoke against production. Gated by TF_ACC=1 like the rest.
func TestAccConfigurationResource_sameRunEnvOverride(t *testing.T) {
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
					// The regression signal: the override must
					// round-trip through the server with the value
					// we sent. If the wrapper or wire shape ever
					// silently drops the override again, this
					// assertion goes red.
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

  # Enablement is per-environment (ADR-055). production is a managed
  # environment seeded on every account and is system-protected — it
  # cannot be deleted, so it's the one stable target the acceptance
  # harness can always rely on (the sibling environment test prunes
  # development, so we don't depend on that slot here).
  environments = {
    production = {
      enabled = true
    }
  }

  # Opt this forwarder into smplkit's own platform change events on
  # create; the update step below flips it back to false to exercise
  # the round-trip.
  forward_smplkit_events = true

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
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "environments.production.enabled", "true"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "forward_smplkit_events", "true"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "configuration.url", "https://splunk.example.com:8088/services/collector/event"),
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "configuration.headers.0.name", "Authorization"),
				),
			},
			{
				Config: testProviderConfig() + fmt.Sprintf(`
resource "smplkit_audit_forwarder" "test" {
  id             = %[1]q
  name           = "Acc Forwarder"
  forwarder_type = "splunk_hec"

  environments = {
    production = {
      enabled = true
    }
  }

  # Flip the platform-change-event opt-in off to confirm the attribute
  # round-trips on update.
  forward_smplkit_events = false

  configuration = {
    url = "https://splunk.example.com:8088/services/collector/event"
    headers = [
      {
        name  = "Authorization"
        value = "Splunk acc-test-token"
      },
      {
        name  = "Content-Type"
        value = "application/json"
      },
    ]
  }
}
`, id),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smplkit_audit_forwarder.test", "forward_smplkit_events", "false"),
				),
			},
			{
				ResourceName:      "smplkit_audit_forwarder.test",
				ImportState:       true,
				ImportStateVerify: true,
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
