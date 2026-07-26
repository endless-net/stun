# EndlessNet STUN

`endlessnet-stun` is the autonomous, stateless STUN infrastructure service used by EndlessNet clients to discover their public UDP mapping. It accepts standard UDP Binding requests and does not depend on the EndlessNet control plane, database, credentials, or internal Go packages.

This repository owns the STUN source, tests, release artifacts, container
image, service configuration examples, and deployment workflow. The public
history in this repository is authoritative. Endpoint selection and
client-side Binding behavior are owned by their respective consumers.

## Boundaries

The service provides unauthenticated STUN Binding over UDP, per-source-IP token-bucket limiting, structured JSON logs, Prometheus metrics, and HTTP health/readiness endpoints. It does not provide TURN, relay user traffic, authorization, billing, coordinator access, PostgreSQL access, DNS discovery, Anycast, or Kubernetes deployment.

Protocol support is documented in [docs/supported-protocol.md](docs/supported-protocol.md). The stable network contract is a standard STUN Binding request and response; repository releases do not change public DNS names or UDP ports.

## Documentation

- [Service architecture, accepted decisions, known limitations, and possible future (Russian)](docs/architecture-and-roadmap.ru.md)
- [Supported STUN protocol](docs/supported-protocol.md)

## Configuration

Flags override environment variables. All configuration is validated before listeners start.

| Environment variable | Flag | Default |
| --- | --- | --- |
| `ENDLESSNET_STUN_ADDRS` | `--addr` | required, comma-separated |
| `ENDLESSNET_STUN_METRICS_ADDR` | `--metrics-addr` | `127.0.0.1:9090` |
| `ENDLESSNET_STUN_RATE_LIMIT_PER_SECOND` | `--rate-limit-per-second` | `20` |
| `ENDLESSNET_STUN_RATE_LIMIT_BURST` | `--rate-limit-burst` | `40` |
| `ENDLESSNET_STUN_LOG_LEVEL` | `--log-level` | `info` |

Use the safe example in `configs/stun.example.env`; no secrets are required. Validate a host configuration without opening sockets:

```sh
set -a
. /etc/endlessnet-stun/stun.env
set +a
/opt/endlessnet-stun/current/endlessnet-stun --check-config
```

## Local development

```sh
go run ./cmd/endlessnet-stun \
  --addr 127.0.0.1:3478 \
  --metrics-addr 127.0.0.1:9090
```

Run the complete quality gate:

```sh
./scripts/verify.sh
```

It runs module verification, unit and process-level integration tests, race tests, `go vet`, formatting checks, and builds the service and smoke-test binaries. A real local Binding request can be checked with:

```sh
./scripts/smoke-test.sh \
  --stun-addr 127.0.0.1:3478
```

## Container

```sh
docker build -t endlessnet-stun:dev .
docker run --read-only --cap-drop ALL --network host \
  -e ENDLESSNET_STUN_ADDRS=0.0.0.0:3478 \
  -e ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:9090 \
  endlessnet-stun:dev
```

The image runs as a non-root user. `deploy/docker/docker-compose.example.yml` pins an exact example version; operational deployments must never use `latest`.

## Local health and observability

- `GET http://127.0.0.1:9090/healthz` reports process health.
- `GET http://127.0.0.1:9090/readyz` returns success only while at least one UDP listener is active.
- `GET http://127.0.0.1:9090/metrics` returns Prometheus text format for the host-local Vector agent.

The HTTP listener is required to use a loopback address. Health, readiness, and metrics are not part of the public network contract; only STUN on UDP port 3478 is exposed externally.

Metrics include `stun_requests_total`, `stun_responses_total`, `stun_invalid_requests_total`, `stun_rate_limited_total`, `stun_errors_total`, `stun_active_listeners`, `stun_request_duration_seconds`, and `stun_build_info`. Labels are limited to listener, result, and address family; source IP is never a label. Successful client addresses and packet bodies are not logged. Rejected request logs redact the remote address.

## Release process

Tags matching `vMAJOR.MINOR.PATCH` run the release workflow. A tag is accepted only when its commit is reachable from `origin/main` and GitHub associates that commit with a merged pull request whose base is `main`. The workflow repeats the quality gate, builds Linux AMD64 and ARM64 server and smoke-test binaries, creates SHA256 checksums, publishes a GitHub Release, and pushes immutable GHCR tags for the exact version, major/minor line, and major line. Deployment always consumes the exact `vMAJOR.MINOR.PATCH` artifact and verifies its checksum.

Update `CHANGELOG.md` on a feature branch, merge it through a pull request and CI into `main`, and only then create a signed or protected version tag. Direct-push and unmerged commits are rejected by both release and production deployment provenance checks. The release job never bypasses the `production` environment.

## systemd deployment

Bootstrap a Linux host once with `sudo ./scripts/install.sh`, review `/etc/endlessnet-stun/stun.env`, and deploy an exact release through `.github/workflows/deploy-production.yml`. Runtime layout:

```text
/opt/endlessnet-stun/
├── releases/v1.0.6/endlessnet-stun
├── releases/v1.0.7/endlessnet-stun
└── current -> /opt/endlessnet-stun/releases/v1.0.7
```

The workflow accepts an exact version, a comma-separated target host/group, matching public STUN endpoints, and the `rolling` strategy. Manual dispatch is accepted only from `main`, and the exact release commit must have merged-PR provenance. Targets update sequentially. Each host atomically sets `ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:9090`, performs checksum verification, configuration preflight, an atomic `current` symlink switch, restart of only `endlessnet-stun.service`, and local readiness. The runner then performs a real public UDP Binding smoke test; it never accesses the host-local HTTP endpoints. The remote operations are implemented by `scripts/configure-metrics-bind.sh` and `scripts/install-release.sh`; provenance, exact-version, checksum, configuration isolation, service isolation, readiness, and rollback behavior are covered by Linux deployment integration tests.

Configure the GitHub `production` environment with `DEPLOY_SSH_PRIVATE_KEY` and `DEPLOY_KNOWN_HOSTS` secrets plus `DEPLOY_USER`. Enable required reviewers when the repository's GitHub plan supports environment protection; merged-PR provenance remains mandatory in the workflow itself. Set `STUN_AUTO_DEPLOY_AFTER_RELEASE=true` plus `STUN_TARGETS` and `STUN_ENDPOINTS` variables only when every successful release should deploy to production. The same workflow remains manually dispatchable from `main`.

## Rollback

Before switching, deployment records the prior immutable release. Failure to start, failed readiness, process exit, or failed public Binding smoke test restores the prior symlink and restarts only the STUN unit. `scripts/rollback.sh` provides the same operation explicitly and repeats readiness plus the real external smoke test. Previous artifacts are never rebuilt.

## Troubleshooting

- Configuration failure: run `endlessnet-stun --check-config` with the environment file loaded.
- UDP timeout: verify DNS, host firewall, security group, and `3478/udp`; TCP reachability does not prove STUN reachability.
- Readiness failure: inspect `systemctl status endlessnet-stun` and confirm at least one configured UDP address can bind.
- Rate limiting: inspect `stun_rate_limited_total` and adjust both rate and burst deliberately.
- Deployment failure: inspect the workflow step for the reported target and rollback version; rollout stops before later targets.

EndlessNet clients continue to obtain endpoints such as `stun1.endlessnet.ru:3478` from signed network maps or control-plane configuration. The client has no dependency on this repository's Go packages or release version.
