# Nexus Session Report

## Executive Summary

This session produced two outcomes:

1. The k3s host networking was repaired so the cluster could function normally.
2. Sonatype Nexus Repository OSS was deployed and validated as a working artifact repository manager for Docker and npm.

This report covers:

- host and cluster issues that blocked deployment
- persistent config changes made on the host
- Nexus deployment architecture
- how Nexus works conceptually
- how users interact with Docker and npm through Nexus
- what was validated live
- what remains to finish for production/public use

## Initial Problem State

The hard part of the session was not Nexus itself. The environment underneath it was unhealthy.

Observed symptoms:

- `coredns` crash-looping
- `local-path-provisioner` crash-looping
- `metrics-server` crash-looping
- Traefik install jobs crash-looping
- pods unable to reach the Kubernetes service IP `10.43.0.1`
- `cni0` bridge losing carrier
- flannel/CNI state unstable

At that point, deploying any stateful application was secondary because the cluster substrate itself was broken.

## Root Cause

The root cause was host-side networking configuration interfering with k3s-created pod interfaces.

The file [/etc/systemd/network/20-ethernet.network](/etc/systemd/network/20-ethernet.network) originally matched all Ethernet interfaces:

```ini
[Match]
Type=ether
```

That caused `systemd-networkd` to manage k3s-created veth interfaces for pods. When k3s attached pod veth interfaces to `cni0`, networkd reconfigured them, destabilized the bridge, and caused pod connectivity to collapse.

That produced the key failure mode:

- pods got IPs
- pods had a default gateway of `10.42.0.1`
- but the bridge state was unstable and service routing failed
- system pods could not reach the Kubernetes service IP `10.43.0.1`

This broke core cluster services and prevented Nexus from being deployable.

## Persistent Host Changes

### 1. Narrowed `systemd-networkd` matching to the real NIC only

File:
[/etc/systemd/network/20-ethernet.network](/etc/systemd/network/20-ethernet.network)

Final contents:

```ini
[Match]
Name=enp1s0

[Network]
DHCP=yes
IPv6AcceptRA=yes

[DHCPv4]
UseDNS=yes
UseNTP=yes
```

Why this matters:

- it prevents `systemd-networkd` from treating pod veths as regular host Ethernet devices
- it leaves k3s CNI-managed interfaces alone
- it stabilizes `cni0` and flannel networking

This is the single most important host fix from the session.

### 2. Added loose reverse-path filtering for k3s/flannel

File:
[/etc/sysctl.d/90-k3s-network.conf](/etc/sysctl.d/90-k3s-network.conf)

Contents:

```ini
net.ipv4.conf.all.rp_filter = 2
net.ipv4.conf.default.rp_filter = 2
```

Why this matters:

- strict reverse-path filtering can break VXLAN/flannel-style traffic patterns
- loose mode is safer for Kubernetes node/pod traffic on this host

### 3. Applied firewall allowances for Kubernetes traffic

These were applied live through `ufw`, not committed into this repo.

Effective intent:

- allow inbound from pod CIDR `10.42.0.0/16`
- allow inbound from service CIDR `10.43.0.0/16`
- allow routed pod traffic to/from `10.42.0.0/16`

Why this matters:

- the host had UFW defaults of deny on `INPUT` and `FORWARD`
- without explicit Kubernetes allowances, pod and service traffic was blocked or unstable

## Recovery Actions

After fixing the host config, stale runtime state still had to be cleared.

Recovery steps performed:

- restarted `systemd-networkd`
- stopped and restarted `k3s`
- deleted stale CNI state under `/var/lib/cni`
- removed stale `cni0` and `flannel.1`
- allowed k3s/flannel to recreate network state

These were recovery operations, not new persistent config.

## Cluster State After Repair

After the host networking fixes:

- CoreDNS became healthy
- local-path-provisioner became healthy
- metrics-server became healthy
- Traefik installed and rolled out successfully
- pod bridge `cni0` stayed up
- pod veths stayed attached to `cni0`
- service routing to `10.43.0.1` started working

Only after that did Nexus deployment become straightforward.

## Nexus Deployment

Deployment manifest:
[/var/home/bupd/code/ks/nexus/nexus-oss.yaml](/var/home/bupd/code/ks/nexus/nexus-oss.yaml)

Resources created:

- namespace `nexus`
- PVC `nexus-data`
- single-replica Deployment `nexus`
- Service `nexus`
- Ingress `nexus`
- Ingress `nexus-docker`

Container image used:

- `sonatype/nexus3:3.90.1`

Storage:

- `20Gi`
- storage class `local-path`
- mounted at `/nexus-data`

Deployment characteristics:

- single-node
- persistent local data
- no HA
- suitable for a basic private artifact manager / registry

## Nexus Access

Temporary hostname used first:

- `nexus.128.140.12.238.nip.io`

Later ingress hostnames were updated to:

- `nexus.bupd.xyz`
- `docker.bupd.xyz`

Important caveat discovered:

- Kubernetes ingress was updated successfully
- public DNS was not aligned yet
- `nexus.bupd.xyz` resolved to Cloudflare, not directly to the host in the expected way
- `docker.bupd.xyz` initially had no DNS resolution

That means:

- cluster-side ingress is ready
- public DNS/proxy still needs to be aligned before external use is clean

## Bootstrap Credentials

Initial admin password was read from:

- `/nexus-data/admin.password`

Initial credentials:

- username `admin`
- generated bootstrap password

Later, the admin password was changed manually during the session by the user to:

- username `admin`
- password `Harbor12345`

## What Nexus Is

Nexus Repository is not just a Docker registry. It is a multi-format repository manager.

It supports ecosystems such as:

- Docker / OCI
- npm
- Maven
- NuGet
- PyPI
- Helm
- Go
- Conan
- RubyGems
- and others

Conceptually, Nexus sits between your users/build systems and package upstreams.

It provides:

- private hosting of internal artifacts
- proxying and caching of public upstream artifacts
- aggregation through group repositories
- UI and API for browsing/admin
- access control and cleanup policy controls

## Repository Types in Nexus

For most supported formats, Nexus uses three core repository types:

### Hosted

Used for content you publish yourself.

Examples:

- private Docker images
- internal npm packages
- internal Maven artifacts

### Proxy

Used to cache or relay an upstream public registry/repository.

Examples:

- Docker Hub
- npmjs
- Maven Central

### Group

Used as a single client-facing endpoint that aggregates multiple repositories.

Examples:

- one Docker group containing hosted + proxy
- one npm group containing hosted + proxy

This is the central operational model of Nexus.

## Why Nexus Is Useful

Reasons to use Nexus:

- private package hosting
- dependency caching
- lower reliance on public upstream availability
- reduced rate limiting against public registries
- controlled internal package source
- improved build reproducibility
- centralized policy and visibility

## How Users Typically Use Nexus

Users generally interact with Nexus through their package clients, not primarily through the UI.

Examples:

- Docker/Podman users pull and push images
- npm users install and publish packages
- Maven users resolve dependencies via Nexus groups

The UI is mostly for:

- administrators
- browsing repositories/components
- repository creation
- security management
- troubleshooting

## Official Routing Guidance Learned During the Session

From Sonatype’s current documentation:

- path-based Docker routing is preferred
- subdomain routing is supported
- port connectors are considered legacy
- mixing routing strategies at the same time is not supported

This directly influenced how Docker was configured later in the session.

## Docker Configuration in Nexus

Docker repositories created:

- `docker-hosted`
- `dockerhub-proxy`
- `docker`

### `docker-hosted`

Purpose:

- store internal images pushed by users/CI

Final relevant settings:

- hosted repository
- write policy `ALLOW_ONCE`
- path-based Docker routing enabled
- no separate connector required for the validated path-based use

### `dockerhub-proxy`

Purpose:

- proxy and cache Docker Hub content

Remote URL:

- `https://registry-1.docker.io`

Final relevant settings:

- proxy repository
- path-based Docker routing enabled
- Docker index type `HUB`

### `docker` group

Purpose:

- single pull endpoint combining:
  - `docker-hosted`
  - `dockerhub-proxy`

Why this is useful:

- users only need one Docker registry URL for reads
- Nexus decides whether content comes from hosted storage or upstream proxy cache

## Docker Authentication Notes

The Docker Bearer Token Realm matters.

During the session, Docker auth behavior was verified through the local Nexus API and realm configuration. The active realms ended up including:

- `NexusAuthenticatingRealm`
- `DockerToken`

This is necessary for proper Docker token/auth workflows.

## Docker Validation Performed

Live validation was done against a temporary local port-forward instead of the public hostname, because public HTTPS and DNS alignment were not fully finished.

Port-forward used:

```bash
kubectl port-forward -n nexus svc/nexus 18081:8081
```

Login:

```bash
podman login --tls-verify=false -u admin -p Harbor12345 127.0.0.1:18081
```

Result:

- login succeeded

### Pull validation through Nexus proxy/group

Command:

```bash
podman pull --tls-verify=false 127.0.0.1:18081/docker/library/busybox:latest
```

Result:

- succeeded
- validated Docker Hub proxying through Nexus

### Upstream image pull for hosted push test

Command:

```bash
podman pull docker.io/library/nginx:alpine
```

Result:

- succeeded

### Retag and push into Nexus hosted repo

Tag command:

```bash
podman tag docker.io/library/nginx:alpine 127.0.0.1:18081/docker-hosted/nginx-alpine:test
```

Push command:

```bash
podman push --tls-verify=false 127.0.0.1:18081/docker-hosted/nginx-alpine:test
```

Result:

- push succeeded

### Delete local hosted tag and pull back from Nexus

Delete:

```bash
podman rmi 127.0.0.1:18081/docker-hosted/nginx-alpine:test
```

Pull back:

```bash
podman pull --tls-verify=false 127.0.0.1:18081/docker-hosted/nginx-alpine:test
```

Result:

- pull-back succeeded

### Integrity check

Image inspection showed:

- hosted pulled-back image ID matched the original `nginx:alpine`
- digest matched as expected for the pushed content

This validated the hosted registry roundtrip end-to-end.

## How Docker Paths Work in Nexus

For public Docker Hub images routed through Nexus group:

```bash
podman pull nexus.bupd.xyz/docker/library/busybox:latest
podman pull nexus.bupd.xyz/docker/library/nginx:alpine
```

For internal hosted images:

```bash
podman push nexus.bupd.xyz/docker-hosted/nginx-alpine:test
podman pull nexus.bupd.xyz/docker-hosted/nginx-alpine:test
```

Important detail:

- official Docker Hub images live under the `library` namespace
- so `nginx:latest` becomes `library/nginx:latest` through Nexus path-based routing

## OCI Images

Nexus Docker registry support also serves OCI-style artifacts under the same registry mechanics. In practical terms for this session:

- pulling `busybox` through the Docker group validated the proxy side of OCI/Docker distribution
- the hosted `nginx:alpine` push/pull roundtrip validated stored registry content behavior

## npm Configuration in Nexus

npm repositories created:

- `npm-proxy`
- `npm-hosted`
- `npm`

### `npm-proxy`

Purpose:

- proxy and cache npmjs packages

Remote URL:

- `https://registry.npmjs.org`

### `npm-hosted`

Purpose:

- store private/internal npm packages

### `npm` group

Purpose:

- single client-facing endpoint containing:
  - `npm-hosted`
  - `npm-proxy`

This matches Sonatype’s recommended model for npm.

## npm Validation Performed

Temporary `.npmrc` used for testing:

```ini
registry=http://127.0.0.1:18081/repository/npm/
_auth=YWRtaW46SGFyYm9yMTIzNDU=
```

The `_auth` value above is the base64 encoding of:

```text
admin:Harbor12345
```

### Metadata lookup through Nexus

Commands:

```bash
npm --userconfig /tmp/nexus-npmrc view lodash version
npm --userconfig /tmp/nexus-npmrc view react version
```

Results:

- `lodash` returned `4.17.23`
- `react` returned `19.2.4`

This validated npm metadata access through the Nexus group.

### Package fetch through Nexus

Command:

```bash
npm --userconfig /tmp/nexus-npmrc pack is-number --pack-destination /tmp
```

Result:

- `is-number-7.0.0.tgz` downloaded successfully through Nexus

This validated npm package retrieval through the Nexus group.

## How Users Should Configure npm

For real client use, users should point npm at the Nexus group URL.

Conceptual `.npmrc`:

```ini
registry=https://nexus.bupd.xyz/repository/npm/
```

If authentication is required:

```ini
registry=https://nexus.bupd.xyz/repository/npm/
//nexus.bupd.xyz/repository/npm/:_auth=<base64 user:password>
```

Typical npm usage through Nexus:

```bash
npm install lodash
npm view react version
npm publish
```

The group repository is the correct client-facing endpoint for reads.

## How Proxying Works in Nexus

Proxy repositories in Nexus work like this:

1. Client asks Nexus for content.
2. If Nexus already has it cached, Nexus serves it locally.
3. If not cached, Nexus fetches it from upstream.
4. Nexus stores the content locally.
5. Future requests can be served from Nexus cache.

This model was validated during the session with:

- Docker Hub content (`busybox`) through `dockerhub-proxy`
- npmjs content (`lodash`, `react`, `is-number`) through `npm-proxy`

Benefits:

- faster repeated downloads
- lower dependency on external service availability
- reduced upstream rate-limit pain
- central control point for clients

## Why Group Repositories Matter

Group repositories are the recommended client entrypoints.

Examples from this session:

Docker group:

- `docker-hosted`
- `dockerhub-proxy`
- exposed as `docker`

npm group:

- `npm-hosted`
- `npm-proxy`
- exposed as `npm`

Why groups are useful:

- users get one URL
- admins can change hosted/proxy composition behind that URL
- client configuration stays simple

## Security / Production Notes

### 1. Docker should use HTTPS publicly

Sonatype’s Docker docs strongly imply a proper HTTPS deployment is the supported target. Public Docker CLI validation was not marked complete in this session because public hostname/TLS were not finalized.

### 2. Do not mix Docker routing styles

Path-based routing, subdomain routing, and port connector routing should not be mixed for the same deployment pattern.

### 3. DNS and ingress are separate concerns

Kubernetes ingress was updated to use `nexus.bupd.xyz` and `docker.bupd.xyz`, but public DNS still controls whether clients actually reach this cluster.

### 4. Admin credentials should not be used for normal client operations

Long term, dedicated service accounts or scoped users should be created for:

- Docker pushes
- npm publishing
- CI access

### 5. Password strength

The session ended with the admin password set to `Harbor12345`, which is weak if the host will be internet-reachable.

## What Was Learned About Nexus

Key learnings from the session:

- Nexus OSS is straightforward to deploy once the Kubernetes substrate is healthy.
- The bigger operational complexity was the host networking, not Nexus manifests.
- Sonatype currently prefers path-based routing for Docker.
- Docker Hub proxying through Nexus works cleanly.
- npm proxying through Nexus works cleanly.
- Nexus REST API is suitable for automation of repository creation.
- Hosted + proxy + group is the central mental model for operating Nexus.
- Public hostname correctness and TLS are as important as the Nexus repo definitions themselves.

## Current State at End of Session

Working inside Nexus:

- UI
- admin login
- Docker hosted repository
- Docker proxy repository to Docker Hub
- Docker group repository
- npm hosted repository
- npm proxy repository
- npm group repository

Validated live:

- Docker login (local port-forward endpoint)
- Docker pull through Nexus proxy/group
- Docker push to Nexus hosted repo
- Docker pull back from Nexus hosted repo
- npm package metadata fetch through Nexus
- npm package tarball retrieval through Nexus

Still needed for a production/public setup:

- align DNS/proxy so `nexus.bupd.xyz` and `docker.bupd.xyz` route correctly
- add proper HTTPS for public Docker and npm clients
- revalidate Docker and npm externally on final hostname
- create non-admin users/tokens for client operations

## Important Files

Deployment manifest:
[/var/home/bupd/code/ks/nexus/nexus-oss.yaml](/var/home/bupd/code/ks/nexus/nexus-oss.yaml)

Host network config:
[/etc/systemd/network/20-ethernet.network](/etc/systemd/network/20-ethernet.network)

Host sysctl config:
[/etc/sysctl.d/90-k3s-network.conf](/etc/sysctl.d/90-k3s-network.conf)

## Recommended Next Steps

1. Fix public DNS/proxy routing for `nexus.bupd.xyz` and `docker.bupd.xyz`.
2. Add HTTPS for the public Nexus hostname.
3. Revalidate Docker and npm from the real hostname, not local port-forward.
4. Create service users or scoped credentials for Docker/npm clients.
5. Add operational policies such as cleanup rules and backup planning.

## Sources Used

- Sonatype reverse proxy guidance:
  https://help.sonatype.com/en/run-behind-a-reverse-proxy.html
- Sonatype Docker registry docs:
  https://help.sonatype.com/en/docker-registry.html
- Sonatype repository scaling/routing docs:
  https://help.sonatype.com/en/scaling-repositories.html
- Sonatype npm registry docs:
  https://help.sonatype.com/en/npm-registry.html
- Sonatype container image tags:
  https://hub.docker.com/r/sonatype/nexus3/tags
