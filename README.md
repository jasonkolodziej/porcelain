# Porcelain

Porcelain is a Go-based system administration dashboard for Fedora CoreOS. The intended shape from the beginning is a systemd socket-activated daemon with Dex-backed authentication, D-Bus-mediated host control, Fiber v3 as the application runtime, server-rendered Go templates for the web UI, and first-class support for storage, networking, firewalling, Podman, diagnostics, ZFS, lm-sensors, and Cloudflare tunnels.

---

## Core Philosophy

**Porcelain** treats the web UI as a thin layer over host services rather than a second control plane. The browser renders state and submits intent; the daemon brokers access to D-Bus services, systemd-managed processes, and secret backends. The scaffold keeps that philosophy intact by centering the runtime around four boundaries:

- a Fiber v3 application server with server-rendered templates
- a D-Bus connection boundary for host service brokering
- a unified secret and certificate interface
- feature modules that can grow without entangling auth, TLS, or bootstrapping

---

## Architecture Overview

```text
┌─────────────────────────────────────────────────────────────────┐
│                          Browser UI                            │
│                    html/template dashboards                    │
└──────────────────────────────┬──────────────────────────────────┘
                         │ HTTPS / mTLS
┌──────────────────────────────▼──────────────────────────────────┐
│                      Porcelain Daemon                          │
│  ┌────────────────┐  ┌────────────────┐  ┌──────────────────┐ │
│  │ Fiber v3 App   │  │ Auth Boundary  │  │ D-Bus Boundary   │ │
│  │ Router / SSR   │  │ Dex Middleware │  │ System Bus       │ │
│  └────────────────┘  └────────────────┘  └──────────────────┘ │
│  ┌────────────────┐  ┌────────────────┐  ┌──────────────────┐ │
│  │ Secret Store   │  │ Certificate    │  │ Module Handlers  │ │
│  │ Interface      │  │ Store / TLS    │  │ Storage / Net /  │ │
│  └────────────────┘  └────────────────┘  │ Podman / ZFS /   │ │
│                                          │ Sensors / CF     │ │
│                                          └──────────────────┘ │
└───────────────┬───────────────────────┬────────────────────────┘
             │                       │
    ┌──────────▼──────────┐  ┌────────▼─────────────────────────┐
    │ Secret Backends      │  │ Host / Platform Integrations    │
    │ memory               │  │ systemd socket activation       │
    │ systemd-creds        │  │ D-Bus services                  │
    │ age        (stub)    │  │ Podman / cloudflared / ZFS      │
    │ vault      (stub)    │  │ Fedora CoreOS packaging         │
    └──────────────────────┘  └──────────────────────────────────┘
```

The key change from the earlier README is that secrets and certificate management are no longer described as a later appendage. They are part of the main control-plane path from process startup onward:

1. configuration selects the secret backend and certificate strategy
2. the app boot path establishes D-Bus reachability for host service brokering
3. the app boot path creates the secret and certificate runtime dependencies
4. the TLS manager loads or bootstraps the serving certificate
5. the Fiber server starts only after auth, D-Bus, and certificate wiring is ready

---

## Project Structure

```text
porcelain/
├── butane/
│   └── porcelain.bu                # Fedora CoreOS bootstrap example
├── cmd/
│   └── porcelain/
│       └── main.go                 # Entrypoint
├── internal/
│   ├── app/
│   │   └── app.go                  # Runtime wiring and boot path
│   ├── auth/
│   │   └── dex.go                  # Dex auth boundary and middleware scaffold
│   ├── certmanager/
│   │   └── lifecycle.go            # Hot-reload TLS lifecycle manager
│   ├── config/
│   │   └── config.go               # Env-based runtime configuration
│   ├── dbus/
│   │   ├── connection.go           # D-Bus connection boundary and peer checks
│   │   └── doc.go
│   ├── modules/
│   │   ├── cloudflare/
│   │   ├── diagnostics/
│   │   ├── firewall/
│   │   ├── network/
│   │   ├── podman/
│   │   ├── sensors/
│   │   ├── storage/
│   │   └── zfs/                    # Feature module package boundaries
│   ├── secrets/
│   │   ├── age.go                  # age backend scaffold
│   │   ├── backend.go              # Backend selection
│   │   ├── certmanager.go          # cert-manager issuer scaffold
│   │   ├── memory.go               # Development secret backend
│   │   ├── selfsigned.go           # Bootstrap certificate issuer
│   │   ├── systemd_creds.go        # systemd-creds backend
│   │   └── vault.go                # Vault backend scaffold
│   ├── server/
│   │   ├── router.go               # Fiber routes and template rendering
│   │   ├── server.go               # Fiber server and mandatory mTLS listener
│   │   └── socket_activation.go    # systemd socket activation support
│   └── templates/
│       ├── base.html               # Shared layout
│       ├── dashboard.html          # Home dashboard
│       ├── modules/
│       │   └── storage.html        # Placeholder module partial
│       └── templates.go            # Embedded template loader
├── pkg/
│   ├── api/
│   │   └── types.go                # UI/API view models
│   └── secrets/
│       └── interfaces.go           # SecretStore and CertificateStore contracts
├── systemd/
│   ├── porcelain.service
│   └── porcelain.socket
└── go.mod
```

This layout intentionally promotes `internal/dbus`, `pkg/secrets`, and `internal/secrets` to first-class parts of the scaffold instead of hiding them under a later implementation phase.

---

## Key Design Decisions

### 1. The Daemon Starts from Secrets and Certificates, Not from Routes

The boot sequence is centered on trust establishment and host control reachability:

1. load configuration
2. connect to the configured D-Bus boundary
3. instantiate the configured secret backend
4. instantiate the configured certificate store
5. bootstrap or load the serving certificate through the TLS manager
6. build the Fiber server and routes

That ordering matters on CoreOS because auth, D-Bus mediation, mTLS, tunnel credentials, and future node-to-node trust all need explicit runtime boundaries from process start.

### 1.1 Fiber Is the Application Runtime, but HTTP/3 Still Belongs at the Edge

The project can be re-architected around Fiber v3 cleanly for routing, middleware, and template responses. That part is straightforward and is now the direction of the scaffold.

HTTP/2 over TLS is exposed through the TLS listener and ALPN. HTTP/3 is different: Fiber itself does not provide a native QUIC server path in this scaffold. If HTTP/3 is a hard requirement, the practical design is an H3-capable edge such as Caddy or Traefik in front of Porcelain, with TLS passthrough or a second mTLS hop depending on deployment constraints.

So the new baseline is:

- no plaintext routes, including development
- mandatory mTLS on every listener
- Fiber inside the daemon
- HTTP/3 handled by the edge layer when needed

### 2. Dex Is a First-Class Boundary Even Though Full OIDC Verification Is Still Ahead

The scaffold includes a dedicated auth package and middleware boundary so Dex integration is not smeared across handlers. The current code focuses on structure rather than production-complete verification:

- auth configuration is explicit and isolated
- middleware is already in the request path
- secret-store access is available to future token and client-secret loading
- downstream handlers consume claims from request context rather than parsing headers themselves

That keeps the future jump from scaffold to full OIDC verification localized to the auth package.

### 3. D-Bus Is an Explicit Host Integration Boundary

Porcelain is meant to broker host capabilities instead of reimplementing them. That only works cleanly if D-Bus access is its own boundary rather than an implementation detail hidden inside modules.

The scaffold now makes room for that explicitly:

- `internal/dbus` owns system-bus discovery and future peer credential checks
- modules can depend on a shared connection manager rather than opening ad hoc sockets
- the security model can describe D-Bus access separately from HTTP auth and secret storage

That matters because the daemon will eventually mediate access to services such as systemd, NetworkManager, UDisks2, and firewalld over the system bus.

### 4. Secrets and Certificates Use Separate Interfaces with a Shared Contract Surface

The new scaffold formalizes two interfaces:

- `SecretStore` for runtime secret persistence, rotation, listing, and watch hooks
- `CertificateStore` for TLS bundle issuance, retrieval, renewal, and trust roots

This separation keeps several deployment models viable without changing the rest of the daemon:

- local development can use the in-memory store and self-signed certificates
- Fedora CoreOS can persist secrets through `systemd-creds`
- air-gapped and external PKI flows can slot into the same boundary through `age`, Vault, or cert-manager backends

### 5. Certificate Lifecycle Is Managed in Process

The TLS manager owns the active certificate in memory and can refresh it without a full process restart. That is the right shape for Porcelain because the daemon is expected to:

- run behind socket activation
- terminate cleanly when idle or updated
- reload serving certificates without tearing down every control-plane session

The current scaffold implements a self-signed bootstrap path and leaves the same runtime slot open for Vault PKI or cert-manager issuance later.

### 6. systemd Socket Activation Remains a Core Boot Assumption

The server supports a systemd-provided listener first and only falls back to a normal TCP listener for development. That preserves the original design goal of a near-zero idle footprint on Fedora CoreOS while keeping the project easy to run on a laptop during early development.

### 7. systemd Hardening and D-Bus Access Must Be Declared Together

The service definition needs to say two things clearly at the same time:

- the daemon is sandboxed tightly enough to fit CoreOS expectations
- the daemon is still allowed to reach the system services it is supposed to broker

That is why the service examples now call out D-Bus access alongside standard hardening directives such as `PrivateTmp`, `ProtectSystem`, and `ProtectHome`.

### 8. Feature Modules Stay Behind Package Boundaries Until Their Host Integrations Are Wired

Storage, network, firewall, Podman, diagnostics, sensors, ZFS, and Cloudflare all have package boundaries in place already. That is deliberate. The goal is to let those modules grow independently while auth, D-Bus, secrets, certificate lifecycle, and serving remain centralized.

---

## Fedora CoreOS Integration

The repository now includes matching CoreOS artifacts in the scaffold itself:

- `systemd/porcelain.socket` for socket activation
- `systemd/porcelain.service` for daemon execution and hardening knobs
- `butane/porcelain.bu` for an ignition-oriented bootstrap example

Those files are still minimal, but they are aligned with the actual server boot path and secrets configuration rather than being purely aspirational snippets.

---

## Local UI Preview

A self-contained Docker workflow renders the dashboard in a real browser
without bypassing mTLS. The image bakes a developer CA, a server certificate,
and a client certificate into `/pki` so the handshake stays mandatory.

```sh
cd porcelain
docker compose up --build
docker compose cp porcelain:/pki/client/client.p12 ./client.p12
# import client.p12 into your browser (password: porcelain), then visit
# https://localhost:8443
```

See `porcelain/docker/README.md` for the full workflow, including how to
import the developer CA into the browser's trusted roots.

---

## Security Model

| Layer | Mechanism | Scaffold Status |
| ----- | --------- | --------------- |
| Transport | Fiber over HTTPS with mandatory mTLS on every listener | Implemented |
| mTLS / trust roots | `CertificateStore` and TLS manager boundary | Scaffolded |
| Authentication | Dex middleware boundary and request claims context | Scaffolded |
| Authorization | Group claims intended to map to admin/operator roles | Scaffolded |
| D-Bus | System bus boundary with room for peer credential enforcement | Implemented in boot path |
| Secrets | `SecretStore` interface with memory and `systemd-creds` backends | Partially implemented |
| Hardening | systemd service sandboxing and syscall filtering | Scaffolded |
| External PKI | Vault PKI / cert-manager issuer boundaries | Scaffolded |

The important architectural shift is that secret and certificate handling now sits inside the security model itself rather than appearing as an optional later enhancement.

---

## Development Roadmap

### Phase 1 — Foundation

- Go module and Fiber-based daemon scaffold
- socket activation-aware Fiber bootstrap
- Dex auth boundary from day one
- embedded server-rendered templates
- D-Bus package boundary for host service brokering
- `SecretStore` and `CertificateStore` interfaces
- development-ready memory backend and self-signed TLS bootstrap

### Phase 2 — CoreOS Trust Path

- production-ready `systemd-creds` flows for service credentials
- Dex OIDC token verification and client-secret loading from the secret store
- mTLS trust bundle handling and client certificate validation
- a dedicated Fiber middleware for mTLS peer identity extraction so the verified
  client certificate (subject CN, SANs, fingerprint) is composed alongside Dex
  claims for downstream handlers
- end-to-end tests exercising the full mTLS handshake against the Fiber router
- a Dockerfile and bootstrap script that issues a server certificate, a client
  certificate, and a developer trust bundle so the UI can be exercised in a
  browser before deployment
- secret watch and rotation semantics for long-lived runtime state

### Phase 3 — Host Control Modules

- shared D-Bus clients for systemd, NetworkManager, UDisks2, and firewalld
- UDisks2-backed storage workflows for local partitions, encryption, RAID, NFS, and iSCSI
- NetworkManager and firewalld integration
- diagnostics collection and system health surfaces

### Phase 4 — Workload and Edge Integrations

- Podman and Quadlet workflows
- ZFS orchestration and event surfaces
- lm-sensors inventory and telemetry views
- Cloudflare tunnel management with encrypted credential handling

### Phase 5 — External Secret and PKI Backends

- age-backed local file encryption
- Vault secret and PKI integration
- cert-manager-backed certificate issuance for containerized deployments

---

## Why This Shape Works

Porcelain now starts from the same assumptions its long-term design requires:

1. trust is established before handlers are exposed
2. Dex, D-Bus, secrets, and certificates are explicit runtime dependencies
3. module growth does not require reworking the boot path
4. CoreOS packaging, socket activation, and HTTPS are represented in code from day one
5. the planned UI can stay sleek and graphical without moving core control logic out of Go

That gives the project a usable scaffold instead of a README-only architecture and leaves the major host integrations ready to fill in behind stable interfaces.
