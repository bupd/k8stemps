# Dragonfly Session Report

## Executive Summary

Dragonfly was deployed successfully into the k3s cluster and exposed at `dragonfly.bupd.xyz`.

The deployment was then exercised in the ways that matter for its actual value proposition:

- registry-mirror style OCI pulls through the Dragonfly proxy
- repeated warm-cache pulls to verify local reuse
- parallel pull pressure to confirm hot-path behavior under concurrency
- generic HTTP package distribution through the proxy
- OCI artifact attachment using referrers
- image signing and signature verification

Bottom line:

- Dragonfly works
- the cache and peer/proxy mechanics are real, not marketing fiction
- the strongest result in this environment was accelerated repeat OCI blob delivery
- some of Dragonfly's bigger claims need a larger multi-node or multi-cluster setup to validate honestly
- the current deployment is functional but not production-hardened yet

## Environment

Cluster:

- k3s `v1.34.5+k3s1`
- single node: `archlinux`
- public node IP: `128.140.12.238`
- ingress controller: Traefik

Dragonfly deployment:

- Helm chart: `dragonfly/dragonfly` `1.6.14`
- Dragonfly app version: `2.4.2`
- namespace: `dragonfly`

Files added for this session:

- [/var/home/bupd/code/ks/dragonfly-values.yaml](/var/home/bupd/code/ks/dragonfly-values.yaml)
- [/var/home/bupd/code/ks/dragonfly-registry.yaml](/var/home/bupd/code/ks/dragonfly-registry.yaml)

## What Was Deployed

Dragonfly components running in-cluster:

- manager
- scheduler
- client daemonset
- seed client
- Redis
- MySQL

Additional test infrastructure:

- a local Docker Distribution registry deployed in-cluster
- NodePort exposure on `128.140.12.238:32000`
- PVC-backed storage for registry content

Public manager access:

- `http://dragonfly.bupd.xyz/`
- `https://dragonfly.bupd.xyz/`

Observed behavior:

- HTTP returned `200 OK`
- HTTPS also returned `200 OK`
- public HTTPS is currently being provided upstream of the cluster by Cloudflare, not by in-cluster Dragonfly TLS configuration

## Configuration Choices

The deployment values intentionally stayed pragmatic for a single-node k3s host:

- `local-path` storage class
- single replica manager
- single scheduler replica
- single seed client replica
- reduced MySQL and Redis storage compared with heavier defaults
- Traefik ingress enabled for the manager UI/API
- explicit JWT signing key configured
- proxy prefetch enabled on client and seed client

The test registry was deployed separately rather than using an external third-party registry so the full push/pull/OCI workflow could be validated end-to-end under local control.

## Problems Encountered And Fixed

### 1. Dragonfly chart sizing was too heavy for this host by default

The stock chart footprint is more generous than needed for a small single-node lab. I reduced persistence sizes and replica counts so the deployment matched the actual machine and storage profile.

### 2. No direct built-in registry to push test content into

Dragonfly accelerates distribution, but it is not itself the origin registry. To validate OCI traffic properly, I deployed a separate local `registry:3` instance in the same namespace and exposed it with a NodePort.

### 3. HTTPS at the public hostname was external to the Helm config

The cluster-side ingress is HTTP-oriented. Public HTTPS worked because the hostname is fronted by Cloudflare. That made the site reachable, but it also means TLS termination is not controlled by the in-cluster Dragonfly deployment.

### 4. ORAS path validation blocked the first SBOM attach attempt

`oras attach` rejected an absolute file path until `--disable-path-validation` was used.

### 5. Cosign v3 was more opinionated than expected

Even with local-key signing against an insecure HTTP registry, `cosign` still emitted transparency-log related behavior and wording that is more online-oriented than older flows. Verification still succeeded with the local public key.

## Validation Results

### 1. Dragonfly manager reachability

Validated:

- `http://dragonfly.bupd.xyz/` returned `200 OK`
- `https://dragonfly.bupd.xyz/` returned `200 OK`
- manager Swagger was reachable at `http://dragonfly.bupd.xyz/swagger/doc.json`

This confirmed that the console and API are publicly reachable.

### 2. Registry availability

Validated:

- `http://128.140.12.238:32000/v2/` returned `200 OK`

This confirmed the local origin registry was usable for push/pull testing.

### 3. OCI image push

Images pushed successfully into the local registry:

- `dragonfly-test/alpine:session-20260312`
- `dragonfly-test/alpine:mirror-20260312`
- `dragonfly-test/busybox:session-20260312`

Resolved digests:

- `dragonfly-test/alpine:session-20260312` -> `sha256:b0cb30c51c47cdfde647364301758b14c335dea2fddc9490d4f007d67ecb2538`
- `dragonfly-test/alpine:mirror-20260312` -> `sha256:b0cb30c51c47cdfde647364301758b14c335dea2fddc9490d4f007d67ecb2538`
- `dragonfly-test/busybox:session-20260312` -> `sha256:70ce0a747f09cd7c09c2d6eaeab69d60adb0398f569296e8c0e844599388ebd6`

### 4. OCI pull through Dragonfly proxy

The proxy was exercised with `skopeo` via `http://127.0.0.1:4001`.

Measured results:

- cold pull of alpine tag: `3.800s`
- warm pull of the same tag: `0.094s`
- warm pull of duplicate tag pointing at same content: `0.106s`

Important observation:

- registry logs showed the cold fetch retrieving blobs from the origin
- later warm pulls only needed manifest requests
- Dragonfly client logs showed blob reuse from local storage

This is the clearest proof from the session that Dragonfly's hot-path registry acceleration is doing real work.

### 5. Parallel pull stress

Stress test executed:

- 8 parallel `skopeo copy` pulls against the same hot image

Observed result:

- all 8 pulls succeeded
- logs showed repeated manifest requests but no repeated backend blob fetch storm
- Dragonfly client logs reported that the blob pieces were already available locally

Assessment:

- for repeated hot content, Dragonfly handled concurrent demand cleanly in this single-node setup

### 6. Generic file/package distribution

A generic HTTP package was downloaded through Dragonfly using the `X-Dragonfly-Use-P2P: true` header.

Test object:

- Debian package `hello_2.10-3_amd64.deb`

Measured results:

- cold download: `2.504s`
- warm download: `1.915s`

SHA256 for both downloads:

- `2e6e2f1a0007dc43bc91c273fd36e91e40a4f1c2765a03eca68b70a42103878a`

Interpretation:

- Dragonfly did correctly proxy and distribute the file
- logs showed the second download being served via the Dragonfly scheduling path and seed peer
- for a tiny 53 KB package, the scheduling overhead offsets much of the acceleration benefit

This means the feature works, but small files are not where Dragonfly shines.

### 7. OCI artifacts and SBOM attachment

Generated:

- CycloneDX SBOM for the pushed Alpine image

Attached successfully with ORAS as an OCI referrer:

- artifact type: `application/vnd.cyclonedx+json`
- attached referrer digest: `sha256:651f521759517369c718b41538a6608d99cf476162ac85b1bf3e6035268604fb`

Discovery results also showed a cosign-related referrer:

- `sha256:6517e8d131835fa3795ff04ba7d8a26e69aa70c1bd63355f16dad55b6407be13`

Assessment:

- OCI referrers worked against the test registry
- Dragonfly can sit in front of this workflow, but the artifact semantics are still really a property of the origin registry plus client tooling

### 8. Image signing and verification

Completed successfully:

- generated a local `cosign` keypair
- signed the Alpine image digest
- verified the signature with the local public key

Verified target:

- `128.140.12.238:32000/dragonfly-test/alpine@sha256:b0cb30c51c47cdfde647364301758b14c335dea2fddc9490d4f007d67ecb2538`

Result:

- signature verification passed
- transparency-log checks also passed in the client output

## What Dragonfly Proved Well

### Strong points

- repeated OCI blob pulls are much faster once content is hot
- proxy-based acceleration is practical and observable
- concurrent warm-cache pulls do not force repeated origin blob downloads
- generic HTTP content can also flow through the same distribution path
- the manager UI and OpenAPI surface are usable enough for operational control

### Where it fits best

Dragonfly makes the most sense when one of these is true:

- the same large images or layers are pulled repeatedly
- multiple nodes or clusters fetch common content
- upstream bandwidth or registry rate limits matter
- large file delivery is a real bottleneck

## What Was Not Fully Proven Here

This cluster is a single-node k3s host. That limits how much of Dragonfly's bigger story can be validated honestly.

Not fully proven in this session:

- large-scale multi-peer swarm efficiency
- multi-node bandwidth offload
- multi-cluster scheduling behavior
- Harbor preheat integration
- AI model distribution use cases
- the broad performance claims from public blog material

The session proved local cache and peer/proxy behavior, not internet-scale or fleet-scale acceleration economics.

## Security And Production Gaps

### 1. Public console credentials were not rotated

The Dragonfly `root` user exists in the manager database, and I did not complete a password rotation during this session.

This is the biggest open operational risk in the current deployment.

### 2. In-cluster TLS is not configured

Public HTTPS currently depends on Cloudflare proxying behavior. A production setup should terminate TLS intentionally, either at the ingress layer in-cluster or in a documented external edge design.

### 3. The test registry is unsecured

The local registry was intentionally exposed over plain HTTP NodePort for validation speed. That is acceptable for this session, not for a hardened public service.

### 4. The registry deployment uses a single replica

This is fine for test traffic, but it has no HA and the registry warning about missing `http.secret` should be cleaned up before any scaled deployment.

### 5. In-cluster MySQL and Redis are convenience services

The official documentation recommends using external MySQL and Redis for more serious deployments. That advice is reasonable.

## Opinionated Assessment

### Is it good?

Yes, in the problem space it targets.

Dragonfly is good when you need distribution acceleration more than you need another full artifact management product. It is not a replacement for Harbor, Nexus, or Gitea. It is a distribution layer that becomes valuable when content is repeatedly fetched at meaningful scale.

### What is good about it?

- the core acceleration path is real
- registry-mirror style blob reuse is effective
- generic HTTP distribution broadens the usefulness beyond containers
- the architecture is operationally understandable: manager, scheduler, client, seed client
- the project is active and, as of January 15, 2026, has reached CNCF graduation according to the official Dragonfly blog

### What is weak or awkward?

- small single-node setups do not showcase its full value
- it adds operational components that are only justified if distribution pain is real
- the product story is broader than the easiest day-1 proof points
- production hardening still requires deliberate work around auth, TLS, observability, and external services
- the tooling around OCI artifacts and signatures still inherits quirks from ORAS, cosign, and the backing registry

### What could be improved?

- a cleaner, more explicit production-hardening guide for small clusters
- sharper guidance for public ingress and auth bootstrap
- clearer end-to-end examples for OCI referrers, SBOMs, and signatures
- better framing around when Dragonfly helps and when it is overkill
- stronger first-party examples for non-container artifact acceleration

## Recommended Next Steps

1. Rotate the Dragonfly root/admin credentials immediately.
2. Put proper TLS in front of the service with an explicit certificate strategy.
3. Move MySQL and Redis to managed or separately operated services if this becomes durable.
4. Add Prometheus/Grafana monitoring using the official Dragonfly monitoring guidance.
5. If you want a more honest benchmark, repeat the pull tests across multiple worker nodes with larger images and real contention.
6. If the target use case includes Harbor or another registry platform, integrate Dragonfly as the acceleration layer in front of that system rather than treating the temporary test registry as permanent.

## Docs And Blog References Used

Official deployment and feature references consulted:

- Dragonfly Kubernetes quick start: https://d7y.io/docs/getting-started/quick-start/kubernetes/
- Dragonfly Helm chart install guidance, including the recommendation to prefer external MySQL and Redis for custom setups: https://d7y.io/docs/v2.0.5/setup/install/helm-charts/
- Dragonfly mirror mode for containerd, showing the registry-mirror/proxy pattern and blob-matching rules: https://d7y.io/docs/v2.0.5/setup/runtime/containerd/mirror/
- Dragonfly preheat guide and OpenAPI usage: https://d7y.io/docs/advanced-guides/open-api/preheat/
- Dragonfly Podman integration docs, which document registry-mirror related headers in newer docs: https://d7y.io/docs/operations/integrations/container-runtime/podman/
- Dragonfly monitoring guidance: https://d7y.io/docs/v2.0.7/concepts/observability/monitoring/
- Dragonfly official blog index: https://d7y.io/blog
- CNCF graduation announcement on the official Dragonfly blog, published January 15, 2026: https://d7y.io/blog/2026/01/15/cncf-announces-dragonfly-graduation/
- Dragonfly v2.4.0 release post on the official blog: https://d7y.io/blog

Local live API reference from the deployed instance:

- `http://dragonfly.bupd.xyz/swagger/doc.json`

## Final Verdict

Dragonfly is good at what it actually is: a distribution accelerator.

In this environment it delivered clear value for repeated OCI pulls and demonstrated that the proxy/seed/cache path is real. It did not magically turn a single-node lab into a dramatic P2P showcase, and it should not be judged that way. If your workload has repeated large downloads across many nodes, Dragonfly is worth serious consideration. If not, it is another moving part you probably do not need.
