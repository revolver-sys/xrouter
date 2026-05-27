# Docker backend isolation lab

This lab introduces the first containerized backend boundary for `xrouter`.

The purpose of this stage is not to move the host gateway daemon into Docker. `xrouter` still runs on the gateway host and remains responsible for host-level routing policy, `pf`/NAT orchestration, TUN readiness checks, health checks, and fail-closed traffic protection.

Docker is used here only for backend isolation.

## Goal

This lab demonstrates that a transport/proxy backend can run as an isolated service while the gateway control plane treats it as a replaceable backend endpoint.

The intended contract is simple:

```text
backend service
   ↓
exposes local endpoint
   ↓
xrouter / transport config can route through that endpoint
   ↓
health checks can verify backend availability
```

This creates the foundation for future stages:

- multiple backend services;
- backend health tracking;
- Redis-backed live/dead state;
- Kubernetes-managed backend deployment;
- pluggable proxy and tunnel backends.

## Boundary

`xrouter` host daemon:

- owns gateway policy;
- applies host firewall/NAT state;
- supervises the current TUN transport process;
- verifies the secure path;
- performs watchdog recovery.

Docker backend services:

- run isolated backend processes;
- expose local ports;
- can be replaced without changing gateway policy;
- can later be managed by Docker Compose, Redis-backed health tracking, or Kubernetes.

## Current stage

This directory is a public-safe lab for Docker backend isolation.

It intentionally does not include real transport server addresses, private keys, UUIDs, tokens, domains, or operational secrets.

Public examples must use documentation-only placeholder addresses such as:

```text
203.0.113.10
198.51.100.10
192.0.2.10
```

## Planned progression

1. Define a neutral backend container service.
2. Expose a local backend endpoint.
3. Add a health-check path for backend availability.
4. Add Redis-backed live/dead backend state.
5. Add Kubernetes deployment manifests for backend services.

This lab establishes the backend-service boundary without changing the current host gateway policy path.
