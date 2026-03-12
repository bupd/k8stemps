# Gitea Registry Session Report

## Executive Summary

Gitea was deployed into the k3s cluster and exposed at `gitea.bupd.xyz`.

What was completed live:

- installed Gitea via the official Helm chart
- enabled the built-in package registry, including the OCI container registry
- exposed the service through Traefik on `gitea.bupd.xyz`
- created an admin account secret in Kubernetes
- authenticated to the registry with Podman
- pushed a test image to Gitea
- removed the local tag and pulled the image back from Gitea

The registry round trip succeeded with this image path:

- `gitea.bupd.xyz/giteaadmin/alpine:session-20260312`

## Deployment Details

Chart and app versions:

- Helm chart: `gitea-charts/gitea` `12.5.0`
- Gitea app version: `1.25.4`

Cluster objects created in namespace `gitea`:

- Deployment/Pod for Gitea
- PostgreSQL StatefulSet
- Ingress for `gitea.bupd.xyz`
- PVC for Gitea shared storage
- PVC for PostgreSQL data
- Secret `gitea-admin-secret` for the bootstrap admin credentials

Storage:

- Gitea shared storage: `20Gi` on `local-path`
- PostgreSQL storage: `10Gi` on `local-path`

Ingress:

- host: `gitea.bupd.xyz`
- ingress class: `traefik`

Deployment values file:

- [/var/home/bupd/code/ks/gitea/values.yaml](/var/home/bupd/code/ks/gitea/values.yaml)

## What Was Validated

### 1. Gitea application health

Validated:

- Helm release reached `STATUS: deployed`
- Gitea pod became `Running`
- PostgreSQL pod became `Running`
- `http://gitea.bupd.xyz/` returned `200 OK`
- Gitea API returned version `1.25.4`

### 2. Registry health

Validated:

- `GET /v2/` returned `401 Unauthorized`
- response included `Docker-Distribution-Api-Version: registry/2.0`
- this is the expected unauthenticated registry behavior

### 3. Push test

Validated with Podman:

- pulled upstream image `docker.io/library/alpine:3.20`
- tagged it as `gitea.bupd.xyz/giteaadmin/alpine:session-20260312`
- pushed it successfully to Gitea

### 4. Pull test

Validated with Podman:

- removed the local Gitea tag
- pulled `gitea.bupd.xyz/giteaadmin/alpine:session-20260312`
- resulting local image ID was `cc9071bd1610`

This proved the image came back from the remote Gitea registry rather than from an untouched local tag.

## Operational Notes

- Admin credentials are stored in Kubernetes secret `gitea-admin-secret` in namespace `gitea`.
- At the time of implementation, this host did not yet resolve `gitea.bupd.xyz`, so a local `/etc/hosts` entry was added for immediate validation.
- In-cluster TLS is not configured in the Helm values file. Public HTTPS may already be working upstream of k3s; that would be external to this chart configuration. This is an inference from the fact that the deployed ingress is HTTP-only while the public site is reachable over HTTPS.

## Crazy Useful Things You Can Do With Gitea

These are the highest-value capabilities beyond plain Git hosting.

### 1. Run CI/CD directly inside your forge

Gitea Actions is built in, supports familiar workflow YAML, and works with `act_runner`.

Practical use:

- build and test every repo on push
- publish Docker/OCI images into the same Gitea instance
- run preview deploys for branches
- gate merges on Actions status

### 2. Use one hostname as a multi-registry software hub

Gitea’s package system supports more than just container images. Official docs describe package registries for many ecosystems, including container, Helm, Maven, npm, NuGet, PyPI, RubyGems, Conda, CRAN, Pub, and more.

Practical use:

- keep source, CI, and artifacts under one auth domain
- publish private base images, Helm charts, npm packages, and Python packages from the same org
- replace several small internal artifact services with one system

### 3. Mirror from GitHub or GitLab, then mirror back out for DR

Gitea supports pull and push mirroring.

Practical use:

- ingest external upstream repos into your own forge
- keep an internal canonical repo while pushing a mirror to GitHub for public visibility
- maintain a disaster-recovery copy in another forge

### 4. Turn starter repos into real templates

Template repositories can expand variables into selected files when generating a new repo.

Practical use:

- scaffold service repos with company defaults
- inject names, descriptions, and module paths automatically
- stamp out repeatable app, infra, or library repos fast

### 5. Use packages plus Actions for fully self-hosted software supply chains

This is where Gitea gets especially strong:

- repo push triggers Gitea Actions
- Actions build image/package
- Actions publish to Gitea package registry
- downstream repos consume artifacts from the same domain

That gives you a compact internal forge + build + artifact loop.

### 6. Add lightweight project management without GitLab-scale overhead

Gitea includes issues, projects, milestones, assignments, time tracking, dependencies, code review, and branch/review workflows.

Practical use:

- use it as the real team system of record instead of just a Git remote
- manage delivery for smaller teams without needing a separate tracker

### 7. Enforce ownership and review routing with CODEOWNERS

Gitea supports CODEOWNERS files.

Practical use:

- auto-route PR reviews to the right team
- protect sensitive paths like `infra/`, `billing/`, or `prod/`
- keep platform teams from becoming manual review dispatchers

### 8. Drive workflows from email and webhooks

Gitea supports incoming email handling and webhooks.

Practical use:

- convert replies into issue activity
- trigger chatops or deployment hooks
- build lightweight ticket intake or approval flows around mail + webhook events

### 9. Use profile repos and docs as an internal developer portal-lite

Gitea supports profile READMEs and repo wikis/docs patterns.

Practical use:

- make org landing pages useful
- expose service ownership, onboarding, and runbooks close to the code
- build a low-friction internal engineering hub without another product

## Best Next Moves

If this instance is going to become a real internal forge, the next steps with the best payoff are:

1. Add in-cluster TLS termination or document the external TLS path explicitly.
2. Add Gitea Actions runners on separate worker capacity.
3. Create an org namespace and move package publishing there instead of a personal namespace.
4. Add SMTP so notifications, password flows, and review loops behave properly.
5. Add SSO/OIDC if this will be shared across a team.
6. Add backups for both the Gitea PVC and PostgreSQL PVC.

## Official References

- Gitea Helm chart source and chart docs: https://gitea.com/gitea/helm-chart
- Gitea overview: https://docs.gitea.com/
- Gitea Actions: https://docs.gitea.com/usage/actions
- Gitea Actions quick start: https://docs.gitea.com/usage/actions/quickstart
- Gitea packages overview: https://docs.gitea.com/usage/packages
- Gitea container registry: https://docs.gitea.com/1.22/usage/packages/container
- Gitea repository features overview: https://docs.gitea.com/usage/repository
- Gitea template repositories: https://docs.gitea.com/usage/template-repositories
- Gitea repository mirroring: https://docs.gitea.com/1.23/usage/repo-mirror
- Gitea email setup: https://docs.gitea.com/administration/email-setup
