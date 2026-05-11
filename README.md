# thoth-operator

Kubernetes operator for managing Aten Security Thoth control-plane configuration from inside customer clusters.

## Documentation

- Aten Security docs: https://docs.atensecurity.com/docs/kubernetes-operator/
- Public runbook: https://github.com/atensecurity/thoth-runbooks/blob/main/onboarding/kubernetes-operator.md

This operator reconciles a `ThothTenant` custom resource and applies desired state to the Thoth headless governance control plane:

- Tenant settings (`/{tenant}/thoth/settings`)
- Optional webhook test (`/{tenant}/thoth/settings/webhook/test`)
- MDM provider upsert (`/{tenant}/thoth/mdm/providers`)
- Optional MDM sync run + polling (`/{tenant}/thoth/mdm/providers/{provider}/sync`)
- Policy bundle provisioning (`/{tenant}/thoth/policy-bundles`)
- Bulk compliance pack assignments (`/{tenant}/thoth/packs/apply`)
- Optional policy sync trigger (`/{tenant}/thoth/policies/sync`)
- Optional governance evidence backfill (`/{tenant}/governance/evidence/thoth/backfill`)
- Optional decision-field backfill (`/{tenant}/thoth/governance/backfill-decision-fields`)
- Optional redacted decision metadata export (`/{tenant}/thoth/governance/decision-metadata/export`)

## Recommended Pattern

Use a hybrid model:

1. Terraform/Pulumi for platform lifecycle and global governance resources.
2. `thoth-operator` for cluster-local day-2 operations (GitOps-driven settings, tenant bootstrap automation, secret rotation alignment).

## Quick Start

```bash
helm upgrade --install thoth-operator ./charts/thoth-operator \
  --namespace thoth-system \
  --create-namespace

kubectl -n thoth-system create secret generic thoth-admin-token \
  --from-literal=token='<THOTH_ADMIN_BEARER_TOKEN>'

kubectl apply -f examples/thothtenant.yaml
```

## Repository Layout

- `api/` — CRD API types
- `controllers/` — reconcile logic
- `internal/thoth/` — Thoth API client with retry/backoff
- `config/` — raw Kubernetes manifests
- `charts/thoth-operator/` — Helm chart distribution
- `examples/` — sample resources

## Configuration

`ThothTenant.spec` key fields:

- `tenantId` (required)
- `apexDomain` (optional, default `atensecurity.com`)
- `apiBaseURL` (optional override; otherwise derived as `https://grid.{tenantId}.{apexDomain}`)
- `authMode` (optional: `auto`/`bearer`/`api_key`; default `auto`)
- `authSecretRef` (required: Kubernetes secret name/key containing admin bearer token)
- `settings` (optional arbitrary JSON map)
- `mdmProvider` (optional provider block)
- `mdmSync` (optional one-shot sync on spec generation change)
- `webhookSettings` (optional typed webhook config + webhook test on apply)
- `policyBundles` (optional list of Cedar/OPA deterministic policies to create/update)
- `packAssignments` (optional list of bulk pack apply operations)
- `policySync` (optional bool to trigger policy sync on generation changes)
- `governanceEvidenceBackfill` (optional block to trigger evidence backfill on generation changes)
- `governanceDecisionFieldBackfill` (optional block to backfill decision evidence fields)
- `decisionMetadataExport` (optional periodic export; defaults to internal Moses collector)

## Decision Metadata Export

`decisionMetadataExport` is designed for model-training pipelines without leaking raw user/tool content:

- Raw content and tool arguments are not exported.
- Sensitive identities are HMAC-SHA256 hashed per tenant.
- Export includes decision context (policy IDs, reason codes, action class, trace IDs, parameter keys).
- By default, payload is delivered to the internal GovAPI collector:
  `POST /:tenant-id/thoth/governance/moses/training/decision-metadata/collect`.
- If `decisionMetadataExport.destinationUrl` is set, payload is delivered to that external endpoint instead.

Use `decisionMetadataExport.authTokenSecretRef` when your external collector requires bearer auth.

## Security Notes

- Store Thoth admin and MDM tokens only in Kubernetes Secrets (never inline in CRs).
- Restrict operator namespace + RBAC scope where possible.
- Rotate secrets and rely on reconciliation for re-application.
- Secret updates are watched; changing referenced secrets triggers immediate reconcile.

## Production Checklist

- Run at least two replicas with leader election enabled.
- Scope `watchNamespace` if each tenant is isolated per namespace.
- Use GitOps for `ThothTenant` resources and token secret rotation.
- Pin the operator image tag and promote tags through staging before production.
- Monitor `Ready` condition and operator logs for reconciliation failures.

## Release Automation

- Public release workflow: `.github/workflows/release.yml`
- Trigger: signed tag push (`vX.Y.Z` or `vX.Y.Z-rcN`) in `atensecurity/thoth-operator`
- GitHub release notes are sourced from the matching section in `CHANGELOG.md`
  (for RC tags, falls back to base version or `Unreleased`).
- Outputs:
  - Multi-arch image: `ghcr.io/atensecurity/thoth-operator:<version>`
  - OCI Helm chart: `oci://ghcr.io/atensecurity/charts/thoth-operator:<version>`
  - Cosign signatures for both image and chart digest

## License

Apache License 2.0.
