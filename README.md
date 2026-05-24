# terraform-provider-smplkit

The official Terraform provider for [smplkit](https://smplkit.com). Manage
configurations, feature flags, audit forwarders, log groups, environments,
and services with Terraform.

The provider is a thin wrapper over the public [Go SDK](https://github.com/smplkit/go-sdk)'s
**management client** — pure CRUD over the documented public API, no
auto-registration side-effects, no hand-rolled HTTP, no internal packages.
See [ADR-052](https://github.com/smplkit/app/blob/main/docs/adrs/ADR-052-terraform.md)
for the design.

## Quick start

```hcl
terraform {
  required_providers {
    smplkit = {
      source  = "smplkit/smplkit"
      version = "~> 1.0"
    }
  }
}

provider "smplkit" {
  # api_key reads from SMPLKIT_API_KEY by default — create one in the
  # smplkit console (Settings → API keys).
}

resource "smplkit_flag" "dark_mode" {
  id      = "dark_mode"
  name    = "Dark Mode"
  type    = "BOOLEAN"
  default = jsonencode(false)
}
```

Full docs are published on the Terraform Registry; see [`docs/`](docs/) for
the source.

## Repository layout

| Path | What it holds |
|------|---------------|
| `internal/provider/` | All resources, data sources, schemas, and SDK glue. |
| `examples/` | Per-resource and per-data-source HCL examples used by `tfplugindocs`. |
| `templates/` | `tfplugindocs` templates (currently just the provider overview page). |
| `docs/` | Generated docs the Terraform Registry ingests — kept in sync via CI. |
| `scripts/smoke*.py` | Bootstrap and teardown for the release-smoke ephemeral account. |
| `scripts/smoke/main.tf` | The smoke-test Terraform config (six resources, one each). |
| `.github/workflows/` | `ci.yml` (build/lint/test/docs check), `release.yml` (semantic-release + GoReleaser), `smoke.yml` (release smoke). |

## Development

### Build and unit test

```bash
make build         # produces terraform-provider-smplkit
make test          # unit tests
make vet           # go vet
make docs          # regenerate docs/
make docs-check    # fail if committed docs differ from generated
```

### Acceptance tests

The acceptance suite (`TF_ACC=1`) runs against a live smplkit platform.

```bash
# Local development against the local platform (ADR-042).
# Requires the four product services + Caddy to be running — see
# ~/projects/.github/platform/ for the orchestration.
export SMPLKIT_API_KEY=sk_api_...
export SMPLKIT_API_URL=http://localhost
make testacc
```

The tests cover create/read/update/delete, `terraform import`, and drift
for each resource. Header values on `smplkit_audit_forwarder` are
deliberately excluded from `ImportStateVerify` because the audit service
encrypts them at rest and reads return `<redacted>`.

### Production smoke

The release smoke job (`.github/workflows/smoke.yml`) provisions an
ephemeral account, runs `terraform apply` / `terraform destroy` against
production, and deletes the account in an `if: always()` step. It uses
the ADR-036 HMAC verification-token mint to short-circuit the email-
verification flow, so the only secret it consumes is `APP_AUTH_SECRET`.

## Publishing

`release.yml` runs on every push to `main`:

1. `semantic-release --dry-run` computes the next version from conventional
   commits.
2. The workflow tags `v$version` directly (semantic-release's own GitHub
   plugin is bypassed — GoReleaser owns the release artifact).
3. GoReleaser builds the platform matrix, signs `SHA256SUMS` with the GPG
   key in `GPG_SIGNING_KEY` / `GPG_SIGNING_PASSWORD`, and creates a GitHub
   release with `terraform-registry-manifest.json` attached. The
   Terraform Registry auto-ingests it.

`GPG_SIGNING_KEY` may be either armored ASCII or base64-encoded armored
ASCII; the release workflow detects the format and decodes if needed
(see `.github/workflows/release.yml`).

## Commit conventions

`fix:` prefix per the org-wide pre-launch SDK convention. Other conventional
types are allowed but should be picked deliberately — see the org-wide
`CLAUDE.md` for the rules.
