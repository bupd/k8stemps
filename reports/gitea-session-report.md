# Gitea Registry Session Report

## Executive Summary

Gitea was deployed into k3s and exposed at `gitea.bupd.xyz`. The instance was then exercised as more than a Git server:

- Git forge
- container registry
- npm registry
- generic package registry
- template-repo generator
- issue/milestone/wiki tracker
- repo migration target
- OCI artifact store for SBOM/signature-related content

The result is clear:

- Gitea is strong as a compact all-in-one internal developer platform
- package workflows worked well
- Git/project workflows worked well
- OCI supply-chain workflows mostly worked, but modern OCI referrers API support is incomplete in this setup

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

### 3. Container push test

Validated with Podman:

- pulled upstream image `docker.io/library/alpine:3.20`
- tagged it as `gitea.bupd.xyz/giteaadmin/alpine:session-20260312`
- pushed it successfully to Gitea

### 4. Container pull test

Validated with Podman:

- removed the local Gitea tag
- pulled `gitea.bupd.xyz/giteaadmin/alpine:session-20260312`
- resulting local image ID was `cc9071bd1610`

This proved the image came back from the remote Gitea registry rather than from an untouched local tag.

### 5. Admin account update

The bootstrap admin state was changed live:

- created admin user `admin`
- changed password to `Harbor12345`
- updated Kubernetes secret `gitea-admin-secret` to:
  - username `admin`
  - password `Harbor12345`
- cleared the forced-password-change flag on the new admin account

### 6. Feature-tour repos

Additional Gitea features were exercised live through the API:

- organization: `skunkworks`
- template repo: `skunkworks/service-template`
- generated repo from template: `skunkworks/customer-api`
- wiki page created for the generated repo
- labels created
- milestone created
- issue created and assigned
- external repo migration tested into `skunkworks/hello-migrated`

Useful URLs:

- org: `http://gitea.bupd.xyz/skunkworks`
- template repo: `http://gitea.bupd.xyz/skunkworks/service-template`
- generated repo: `http://gitea.bupd.xyz/skunkworks/customer-api`
- generated repo wiki: `http://gitea.bupd.xyz/skunkworks/customer-api/wiki/Runbook`
- migrated repo: `http://gitea.bupd.xyz/skunkworks/hello-migrated`

### 7. npm registry test

Validated:

- generated an access token for `admin`
- published package `@admin/hello-npm@1.0.0`
- installed that package from Gitea
- executed the installed package successfully

Outcome:

- npm publish succeeded
- npm install succeeded
- runtime output matched expected package contents

### 8. Generic package registry test

Validated:

- uploaded generic package file `artifact.txt` to package `session-bundle` version `1.0.0`
- downloaded the same file back from Gitea
- SHA256 hashes matched exactly

Outcome:

- generic package upload/download worked correctly

### 9. SBOM attach, signing, and referrers behavior

Validated on image:

- `gitea.bupd.xyz/admin/alpine:session-20260312`

Live steps completed:

- pushed the image under the `admin` namespace
- resolved its digest:
  - `sha256:1ae801d135528fb118e8b27b757e83d3df9df7780de333a9e31c411dfcf9e373`
- generated a CycloneDX SBOM with `syft`
- attached the SBOM as an OCI artifact with `oras`
- generated a local `cosign` keypair
- signed the image with `cosign`
- verified the signature with the generated public key

Important result:

- OCI artifact storage worked
- cosign signature verification worked
- OCI 1.1 referrers API discovery returned `unsupported`
- legacy/tag-based referrer discovery worked

This is the key supply-chain finding from the session.

## Package Inventory Observed

Listing packages through the Gitea API showed at least these package types live in the instance:

- `container`: `alpine`
- `npm`: `@admin/hello-npm`
- `generic`: `session-bundle`

## Operational Notes

- Admin credentials are stored in Kubernetes secret `gitea-admin-secret` in namespace `gitea`.
- At the time of implementation, this host did not yet resolve `gitea.bupd.xyz`, so a local `/etc/hosts` entry was added for immediate validation.
- In-cluster TLS is not configured in the Helm values file. Public HTTPS may already be working upstream of k3s; that would be external to this chart configuration. This is an inference from the fact that the deployed ingress is HTTP-only while the public site is reachable over HTTPS.
- Some registry tooling hit certificate/transport awkwardness when attempting direct HTTPS registry access. For the live validation, plain HTTP and `--tls-verify=false` / equivalent test flags were used.
- The OCI referrers API itself appears unsupported on this registry path even though artifact attachment and legacy referrer discovery worked.

## Opinionated Assessment

### What Gitea is good at

- consolidating Git, issues, wiki, packages, and light project management under one auth domain
- giving a small team a real forge without GitLab-scale weight
- serving as an internal software hub where source, packages, and CI concepts live together
- handling practical package flows like container images, npm packages, and generic artifacts
- bootstrapping standardized repos fast with template repositories

### What Gitea is bad at or awkward at

- the supply-chain story is not as smooth as a dedicated registry platform when you push into OCI edge cases
- modern OCI referrers API behavior is not there in a clean way in this setup
- package UX is serviceable, but not as specialized or polished as a dedicated artifact manager
- some workflows still assume you know the package path conventions and auth model already
- chart defaults still need deliberate hardening for production, especially around cache/session/queue backends and TLS clarity

### What surprised me positively

- npm registry support was easier than expected once a token existed
- template repo generation is genuinely useful, not just a demo feature
- generic packages make Gitea more practical than people usually give it credit for
- for a single service, Gitea covers a lot of day-1 and day-2 developer workflow surface area

### What surprised me negatively

- referrers API support appears incomplete even though OCI artifacts can still be stored
- registry transport behavior is touchier than it should be when using external tooling
- the admin bootstrap flow is fine for deployment, but account management becomes manual quickly if you are changing identities after the fact

## Good / Bad Summary

Good:

- fast to stand up
- broad feature surface
- strong value density for one service
- package support is real, not cosmetic
- good fit for self-hosted internal platforms

Bad:

- OCI advanced workflows are not first-class enough yet
- registry UX and compatibility are not at Harbor/Nexus depth
- production hardening still needs more work than the happy path suggests
- auth and token ergonomics are functional but not elegant

## What We Can Take From It

The practical takeaway is not “replace every specialist tool with Gitea.”

The practical takeaway is:

1. Gitea is an excellent central forge for source, issues, templates, wikis, and common package types.
2. Gitea is good enough for many internal package workflows, including container, npm, and generic artifacts.
3. If you care deeply about advanced OCI registry behavior, signatures, referrers, provenance, and ecosystem compatibility, a dedicated registry like Harbor or Nexus still gives you a cleaner long-term story.
4. The strongest architecture is likely:
   - Gitea as the forge and collaboration system
   - Harbor or Nexus as the heavyweight artifact/registry plane when OCI depth matters
5. If you want fewer moving parts and mostly straightforward developer workflows, Gitea can carry more load than most teams expect.

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
