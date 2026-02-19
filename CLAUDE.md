# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What This Repo Is

A collection of Kubernetes manifests, Helm values, and tooling used for learning, lab work, and deploying services across Kind, K3s/K3d, and Talos clusters. Not a single application; it is a flat collection of YAML files and small utilities.

## Repository Layout

- Root YAML files: standalone K8s manifests (pods, deployments, services, ingress, cert-manager issuers, RBAC, etc.)
- `depl/`: Harbor Helm chart deployments with various configurations (8gears, upstream, oidc, sar, etc.). Each subdirectory has its own `values.yaml`.
- `credential-provider-plugin/`: Go binary implementing the Kubelet credential provider API (v1). Echoes service account tokens as registry passwords. See its `README.md` for usage.
- `chart/`: A simple custom Helm chart (deployment + service + ingress).
- `keycloak/`: Docker Compose setup for Keycloak with Postgres.
- `immich/`: SOPS-encrypted secrets for Immich deployment (uses age encryption).
- `grafana-dashboard/`: Harbor monitoring dashboard JSON.
- `destroy/`: Dockerfile + nginx config for a static page.
- `talos/`, `test-talos/`: Talos Linux machine configs (gitignored, contain cluster secrets).

## Credential Provider Plugin (Go)

Located in `credential-provider-plugin/`. Build and test:

```sh
cd credential-provider-plugin
go build -o credential-provider-echo-token .
```

The binary reads a `CredentialProviderRequest` from stdin and returns a `CredentialProviderResponse` with the SA token as the registry password. Used with Kind clusters via a custom node image (see root `Dockerfile` and `kind-config.yaml`).

## Kind Cluster Setup

The root `Dockerfile` extends `kindest/node` to include the credential provider plugin binary. `kind-config.yaml` enables the `KubeletServiceAccountTokenForCredentialProviders` feature gate.

## Secrets Handling

This is a public repo. The `.gitignore` blocks sensitive files:
- `certmanager-issuer-secret.yaml` (Cloudflare API token)
- `new8gcr-dev-reg-pullsecret.yml` (registry credentials)
- `harbor-token.jwt`, `jwks.json`, `openid-configuration`
- `talosconfig`, `talos/`, `test-talos/` (CA keys and cluster secrets)
- `keycloak/.env` (admin passwords)
- `controlplane.yaml`, `worker.yaml`, `config-control-plane.yaml` (Talos machine configs)
- Compiled Go binaries in `credential-provider-plugin/`

SOPS with age is used for Immich secrets (`immich/immich-secret.yaml`).

Before committing any new YAML, check if it contains hardcoded credentials, tokens, or base64-encoded secrets. If so, add it to `.gitignore`.
