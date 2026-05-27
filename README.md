# xrouter

**xrouter** is a Go-based LAN gateway control plane for secure transport backends, `pf`/NAT orchestration, and fail-closed traffic protection.

The current implementation provides a working macOS gateway daemon that supervises a TUN-based transport process, detects active `utun` interfaces, applies `pf` NAT and fail-closed policy, performs health checks, and triggers deterministic recovery when the configured secure path becomes unavailable.

The long-term goal is to evolve xrouter into a modular local-network traffic control platform with pluggable transport backends, containerized backend isolation, live node status tracking, event streaming, and deployment models for larger LAN and edge-network environments.

## Current working stage

The current implementation is a macOS gateway daemon that:

- supervises a TUN-based transport process;
- detects active `utun` interfaces;
- applies `pf` NAT and fail-closed traffic policy;
- runs health checks against a configurable endpoint;
- triggers deterministic recovery when the secure path fails;
- exposes status/debug information for operational inspection.

The current transport integration uses an external TUN transport backend. The control path is being structured around backend-neutral supervision so future proxy and tunnel backends can use the same lifecycle, health-check, and recovery model.

## Active development direction

The current implementation is not a standalone experiment. It is the first working stage of a broader local-network routing platform.

The project is being extended toward:

- pluggable proxy and tunnel backends;
- Docker-based backend isolation;
- Redis-backed live/dead status tracking;
- Kafka event streams for routing and recovery events;
- PostgreSQL-backed configuration and audit history;
- Kubernetes deployment models for containerized backend services;
- multi-node and multi-user local-network deployments;
- eventually, a native tunnel client.

## Operational model

xrouter separates gateway control from transport implementation.

At the current stage, the daemon coordinates:

- gateway setup scripts;
- transport process supervision;
- `utun` readiness detection;
- dynamic `pf` policy application;
- LAN NAT through the active transport interface;
- fail-closed protection for LAN clients;
- watchdog recovery after repeated health-check failures.

The intended safety model is fail-closed: when the configured secure transport path is unavailable, LAN traffic should not silently fall back to an unsecured WAN path.

## Architecture

Current high-level flow:

```text
LAN clients
   ↓
macOS gateway host
   ↓
xrouter
   ├── transport process supervision
   ├── utun readiness detection
   ├── pf/NAT policy orchestration
   ├── health checks
   └── watchdog recovery
   ↓
TUN transport backend
   ↓
secure transport path
```

The project currently uses shell scripts for host-level pf, interface, and gateway operations. The Go daemon owns lifecycle coordination, config loading, process supervision, status reporting, and recovery decisions.

## Commands

```zsh
xrouter up       start gateway policy and transport supervision
xrouter down     stop gateway policy and restore normal network state
xrouter run      run watchdog and recovery loop
xrouter status   show gateway, transport, pf, and health status
xrouter -h       show help
```

Most operational commands require root privileges because they interact with pf, network interfaces, routing state, and host-level gateway configuration.

Example:

```zsh
sudo ./bin/xrouter -config ~/xrouter/config/xrouter/config.yaml status
```
## Configuration

A sanitized root-level example is provided in `config.example.yaml`. The same structure is shown below for quick review.

## Build

Native build:

```zsh
make build
```

Run status:
```zsh
sudo ./bin/xrouter -config ~/config_path/config.yaml status
```
Run watchdog/recovery loop:
```zsh
sudo ./bin/xrouter -config ~/config_path/config.yaml run
```
## Project origin

xrouter is a public, reorganized continuation of an earlier working LAN gateway prototype.

The original prototype validated the core gateway mechanics: process supervision, TUN readiness detection, pf NAT orchestration, health checks, watchdog recovery, and fail-closed LAN protection.

This repository starts from that working base and restructures the project around a broader architecture: pluggable transport backends, local-network policy control, containerized backend services, and future multi-node operation.

## License

Apache License 2.0.

## config.example.yaml

```yaml
# xrouter example configuration
#
# This file is public-safe and intentionally uses documentation-only
# placeholder addresses. Do not commit real server IPs, private keys,
# UUIDs, tokens, domains, or local operational secrets.

# Host interfaces.
# Adjust these for the gateway machine.
wan_if: "en7"
lan_if: "en5"

# Gateway control scripts.
# These scripts apply host-level interface, forwarding, DNS/DHCP, and pf policy.
gateway_setup_path: "./scripts/gateway_setup.sh"
gateway_pf_apply_path: "./scripts/gateway_pf_apply.sh"
gateway_down_path: "./scripts/gateway_down.sh"

# Health check.
# The watchdog uses this endpoint to verify that the configured secure path is alive.
health_check_url: "https://api.ipify.org?format=text"
check_interval: 10s
health_timeout: 5s
failure_threshold: 3
recover_cooldown: 5s
max_recoveries: 5

# Script/process command timeout.
command_timeout: 20s

# Current TUN transport backend.
# The current implementation supervises an external TUN transport process.
# Future stages will generalize this into pluggable proxy and tunnel backends.
transport_path: "/path/to/transport/backend"
transport_config_path: "/path/to/transport/config.json"

transport_auto_start: true
transport_auto_stop: true
transport_adopt_external: true
transport_start_timeout: 8s
transport_stop_timeout: 8s

# Runtime files for the supervised transport process.
transport_pid_file: "/path/to/runtime/transport.pid"
transport_log_file: "/path/to/runtime/transport.log"

# Fail-closed WAN allowlist.
# These are documentation-only placeholder addresses.
# Real transport server IPs belong only in a private local config file.
transport_server_ips:
  - "203.0.113.10"

# Optional WAN DNS allowlist for controlled resolver access.
wan_dns_ips:
  - "1.1.1.1"
  - "8.8.8.8"

# Optional WAN NTP allowance.
allow_wan_ntp: true
```

