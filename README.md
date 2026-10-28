# STUN — самостоятельный UDP Binding сервис

Сервис возвращает наблюдаемый сервером source IP/port в XOR-MAPPED-ADDRESS.
Работает без Account, credentials, БД, SPIRE и control plane; не пересылает payload.
Это ограниченный wire subset RFC8489, а не полная реализация STUN/TURN.
[Wire profile и offline vectors](api/README.md) выпускаются независимо от runtime.

## Запуск и конфигурация

```bash
./stun --listen '0.0.0.0:3478,[::]:3478' --http-listen '127.0.0.1:8080'
./stun --listen '127.0.0.1:3478' --check-config
./stun --version
```

--listen — обязательный список 1–4 числовых IP:port через запятую, без DNS,
нулевых ports, duplicates, IPv4-mapped IPv6 и zone identifiers. Семейства
используют отдельные sockets; ошибка одного обязательного bind отменяет весь старт.
--http-listen по умолчанию 127.0.0.1:8080 и принимает только loopback IP.
Flags перекрывают ENDLESSNET_STUN_LISTEN, ENDLESSNET_STUN_HTTP_LISTEN,
ENDLESSNET_STUN_OTEL_ENDPOINT. --log-level / ENDLESSNET_STUN_LOG_LEVEL
принимает debug/info/warn/error (default info); --log-format /
ENDLESSNET_STUN_LOG_FORMAT принимает json (default). Версия и проверка config не открывают sockets.
Некорректная конфигурация даёт exit2; startup/fatal failure — exit1; SIGINT/SIGTERM —
bounded stop и exit0. Ошибки выводятся безопасными кодами без исходных addresses.

Optional --otel-endpoint принимает явный полный OTLP HTTP URL, например
http://127.0.0.1:4318/v1/traces. Без него внешнего экспорта нет. Ambient OTEL resource attributes,
endpoints/headers не заменяют явную конфигурацию; retries и redirects выключены,
HTTPS требует TLS1.3. Экспортёр не является обязательной зависимостью Binding.

## Малый профиль

- Datagram ≤1500 bytes; invalid/oversized/unknown required/unsupported — silent drop.
- Source IP/listener: 20/s, burst40, ≤65536 buckets; ports не разделяют budget.
- Idle TTL300s, lazy sweep не реже одного раза в300s при входящих пакетах;
  rejected packets обновляют lastSeen. Новый IP при заполнении таблицы отбрасывается.
- Один UDP worker и buffer1501/listener, без request goroutine/application queue.
- Startup5s, UDP write100ms, общий shutdown5s; fail-fast при потере listener.
- HTTP ≤32 connections, 64KiB headers, body0, read/header/write5s, idle60s.
- Log queue256, drop-new, packet logs ≤10/s/burst20; JSON slog без client metadata.
- OTel sample1%, cap100 spans/s, queue1024, batch128, interval1s, timeout2s, retries0.
- Fixed metrics без client labels; queues и их high-water/drop/failure counters видимы.
- Process acceptance: ≤128 goroutines и RSS≤256MiB. Go memory target192MiB —
  мягкая GC-настройка; фактический RSS проверяется отдельно, не обещает Internet SLO.

GET /healthz — процесс жив; GET /readyz — все обязательные listeners готовы;
GET /metrics — агрегированные счётчики. Только loopback; unknown path404,
other method405, body/Transfer-Encoding413. Readiness не доказывает remote endpoint,
NAT traversal, peer traffic или права доступа. Логи не содержат IP/port клиента,
transaction bytes, payload или raw net errors; локально созданный trace_id допустим.

## Проверки и выпуск

Из корня в Ubuntu26.04 с Go1.27.3 и pinned tools:

```bash
python3 -B scripts/test_ci.py
python3 -B scripts/ci.py check
go test -race -short ./...
```

Отдельная runtime-приёмка, вне базового unit-only CI:

```bash
go test -race -tags=integration ./internal/server -timeout=40s
go build -o /tmp/stun-runtime ./cmd/stun
STUN_BINARY=/tmp/stun-runtime python3 -B test/acceptance/process_test.py
bash test/acceptance/run.sh
```

Последняя команда создаёт только временный user/network namespace, назначает
локальные IPv4/IPv6 test sources и выполняет полный профиль860s с deadline15min:
normal/saturation/recovery, queue/HTTP pressure, idle/sweep. Нужны Linux user
namespaces, unshare и iproute2. Test-only sampler100% при том же cap100/s нужен
для достижения queue pressure; production flag для него отсутствует.
WSL Ubuntu userland и loopback не подтверждают production kernel/NAT/регионы.

Clean committed build: python3 -B scripts/ci.py build. Профиль модулей/артефактов
в [.ci/profile.json](.ci/profile.json); output — ignored .artifacts или вне checkout.
Public CI автономен, GitHub-hosted Ubuntu26.04, без private Service Kit.
Изменения публикуются только в main; новый runtime v0.* tag ставится после successful
exact main CI. Tag CI повторяет checks и автоматически выпускает Linux amd64 archive,
checksums.txt и source.json. Publisher проверяет fresh main ancestry и main push CI.
api/v0.* source bundle выпускается независимо; подходящий неизменённый API используется
повторно. Исторические теги immutable, v0 не обещает backward compatibility.
Deployment/host activation и общая EndlessNet peer-traffic acceptance сюда не входят.
