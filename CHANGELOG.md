# Changelog

All notable changes are recorded here. Releases use semantic versioning.

## Unreleased

- Keep UDP listeners alive after truncated datagrams, sanitize transport errors,
  and validate complete Binding responses.
- Add required standalone product E2E for Linux, Windows and Linux containers,
  with independent wire checks and exact-commit release evidence gates.
- Document the standalone product and separate D-026 Infrastructure lifecycle.

- Remove STUN-owned production activation, rollout, rollback, SSH credentials,
  and host mutation; publish immutable artifacts for Infrastructure handoff
  through the released manifest.

## v1.0.9 - 2026-07-14

- Retry host-local readiness during service startup so rolling deploys tolerate the brief transition before UDP listeners become ready.

## v1.0.8 - 2026-07-14

- Restrict health, readiness, and Prometheus metrics to the host loopback interface for local Vector collection; production smoke tests now verify only the public STUN UDP endpoint.

## v1.0.7 - 2026-07-13

- Require every production release and deployment commit to come from a merged pull request into `main`; manual deployment dispatches must also run from `main`.

## v1.0.6 - 2026-07-13

- Retry host-local readiness with the configured source address and owning network interface when self-connect firewall rules reject an unbound request.
- Add an explicit, atomic deployment migration for the metrics bind address and pass workflow inputs through environment variables to prevent command injection.

## v1.0.5 - 2026-07-13

- Include host-interface addresses in local readiness probes for STUN nodes behind public NAT.
- Report the sanitized metrics listening socket when every host-local HTTP route fails.

## v1.0.4 - 2026-07-13

- Probe configured, loopback, wildcard, and IPv6-loopback candidates for host-local readiness when public-address hairpin connections are unavailable.

## v1.0.3 - 2026-07-13

- Move remote installation into a versioned, directly tested script and use an unpredictable remote upload path.
- Verify checksum rejection, atomic version switches, STUN-only restarts, readiness, and automatic rollback in Linux CI.
- Normalize wildcard metrics bind addresses to loopback for local readiness checks.

## v1.0.2 - 2026-07-13

- Handle a missing `current` symlink correctly during the first systemd deployment.

## v1.0.1 - 2026-07-13

- Verify readiness and a real external STUN exchange after every automatic rollback path.
- Avoid changing the active release when a deployment fails before its atomic symlink switch.
- Cross-compile multi-architecture container binaries on the native build platform.
- Key Go dependency caches from `go.mod` and keep the vulnerability scan on the declared Go toolchain.

## v1.0.0 - 2026-07-13

- Establish the reviewed standalone STUN service baseline.
- Add autonomous configuration, rate limiting, health/readiness, Prometheus metrics, structured logging, tests, container packaging, releases, rolling systemd deployment, public smoke testing, and rollback.
