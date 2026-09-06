# Артефакт systemd и передача в Infrastructure

Этот репозиторий публикует immutable STUN-артефакты. Он не выбирает production
узлы, не имеет inventory или host credentials и не выполняет activation,
rollout или rollback. Эти операции принадлежат Infrastructure согласно D-014
и D-025 и запускаются единым released manifest.

## Commit-addressed publication

Workflow `.github/workflows/publish-production.yml` принимает полный SHA
коммита из `main` и требует green CI для application commit и publisher
workflow. Он публикует Actions artifact `endlessnet-stun-<commit_sha>` с:

- AMD64 и ARM64 бинарниками STUN и smoke-клиента;
- systemd unit и license notices;
- SHA-256 archive checksum;
- schema-v1 manifest, связывающий архив с commit и CI runs;
- in-toto/SLSA provenance и Sigstore/Rekor bundle.

Publisher проверяет layout, checksum, manifest и provenance до upload. В нём
нет target-параметров, SSH-секретов и host mutation.

## Handoff

Infrastructure должна забрать exact artifact из released manifest, проверить
его digest и применить собственные процедуры подготовки хоста, activation,
rolling rollout, readiness/smoke gates и rollback. STUN не поставляет скрипты
удалённой установки и не является orchestrator-ом production.

Архив содержит systemd unit как декларативный runtime artifact. Его примерная
конфигурация использует loopback для `/healthz`, `/readyz` и `/metrics`, а
публичный контракт сервиса — только STUN Binding по UDP.

Локальный smoke-клиент можно запускать для проверки уже активированного
Infrastructure endpoint:

```sh
./bin/endlessnet-stun-smoke --stun-addr stun.example.com:3478 --timeout 5s
```

Любые production failure и rollback расследуются в Infrastructure по released
manifest и rollout telemetry; в STUN изменяется только следующий immutable
artifact.
