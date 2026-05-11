# Changelog

All notable changes to `thoth-operator` are documented in this file.

## Unreleased

- No changes yet.

## 0.1.0 - 2026-05-05

### Added

- Initial public `thoth-operator` release with `ThothTenant` reconciliation.
- Kubernetes-native reconciliation for tenant control-plane configuration:
  - tenant settings
  - webhook settings with optional test-on-apply
  - MDM provider upsert and optional sync orchestration
  - deterministic policy bundle provisioning (OPA/CEDAR)
  - bulk regulatory pack assignments
  - policy sync trigger support
  - governance evidence backfill trigger
  - governance decision-field backfill trigger
- Redacted decision metadata export pipeline for training/analytics:
  - `GET /:tenant-id/thoth/governance/decision-metadata/export`
  - `POST /:tenant-id/thoth/governance/moses/training/decision-metadata/collect`
  - per-tenant HMAC hashing for sensitive identity fields
  - no raw content/tool-argument export
- Internal collector default for decision metadata export with optional external destination URL + bearer token secret reference.
- Provisioning attribution headers on operator-managed API calls so GovAPI/dashboard can classify operator-driven changes.
- Public Helm chart + operator examples + onboarding/production runbooks for customer deployment and upgrades.
