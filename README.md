# thoth-operator

Kubernetes operator for managing Aten Security Thoth control-plane configuration from inside customer clusters.

## Documentation

- Aten Security docs: https://docs.atensecurity.com/docs/kubernetes-operator/
- Public runbook: https://github.com/atensecurity/thoth-runbooks/blob/main/onboarding/kubernetes-operator.md

This operator reconciles a `ThothTenant` custom resource and applies desired state to the Thoth headless governance control plane:

- Tenant settings (`/{tenant}/thoth/settings`)
- MDM provider upsert (`/{tenant}/thoth/mdm/providers`)
- Optional policy sync trigger (`/{tenant}/thoth/policies/sync`)

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
- `authSecretRef` (required: Kubernetes secret name/key containing admin bearer token)
- `settings` (optional arbitrary JSON map)
- `mdmProvider` (optional provider block)
- `policySync` (optional bool to trigger policy sync on generation changes)

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
- Outputs:
  - Multi-arch image: `ghcr.io/atensecurity/thoth-operator:<version>`
  - OCI Helm chart: `oci://ghcr.io/atensecurity/charts/thoth-operator:<version>`
  - Cosign signatures for both image and chart digest

## License

Apache License 2.0.
