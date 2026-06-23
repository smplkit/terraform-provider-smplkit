# terraform-provider-smplkit

The official Terraform provider for [smplkit](https://smplkit.com). Manage
configurations, feature flags, audit forwarders, log groups, environments,
services, and scheduled jobs with Terraform.

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
| `.github/workflows/` | `ci.yml` (build/lint/test/docs check), `release.yml` (semantic-release + GoReleaser). |

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

It is **destructive** — it deletes the authenticating account's seeded
`development` environment to free a managed slot (ADR-051) — so locally it
must run as a dedicated, isolated throwaway account, never your dev/preview
account (ADR-052 §2.8).

```bash
# Local development against the local platform (ADR-042). Requires the
# product services + Caddy + the app service running — see
# ~/projects/.github/platform/ for the orchestration.
#
# Provision the dedicated, isolated local-acceptance account once:
python3 ~/projects/.github/platform/seed-acceptance-account.py
#
# Then run against it. testacc-local reads the [local-acceptance] profile
# from ~/.smplkit and sets SMPLKIT_API_URL + SMPLKIT_ACC_DESTRUCTIVE=1.
make testacc-local
```

See `~/projects/.github/docs/local-testing.md` for the full story. The
prod-only `make testacc` target (used by the e2e suite against an ephemeral
production account) is unchanged and still requires an explicit
`SMPLKIT_API_URL`.

The tests cover create/read/update/delete, `terraform import`, and drift
for each resource. Header values on `smplkit_audit_forwarder` are
deliberately excluded from `ImportStateVerify` because the audit service
encrypts them at rest and reads return `<redacted>`.

### Production smoke

Production smoke lives in a separate private repo,
[`terraform-provider-smplkit-smoke`](https://github.com/smplkit/terraform-provider-smplkit-smoke).
The split is a trust-boundary choice: the smoke needs the
production `APP_AUTH_SECRET` (the platform's JWT signing key) to mint
a verified throwaway account, and hosting that workflow in a public
repo would put the secret one workflow-injection PR away from a
leak. The private repo runs the smoke daily and on manual dispatch,
against the latest GitHub Release of this provider.

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
