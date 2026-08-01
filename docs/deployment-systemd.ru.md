# Развёртывание EndlessNet STUN через systemd

## Что разворачивается

Релизный пакет `endlessnet-stun_<version>_linux_<arch>.tar.gz` содержит:

- статически собранный Linux-бинарник `bin/endlessnet-stun`;
- smoke-клиент `bin/endlessnet-stun-smoke` для настоящего STUN Binding-запроса;
- systemd unit, пример конфигурации и скрипты установки;
- `SHA256SUMS` для проверки содержимого пакета.

Выберите пакет по архитектуре сервера:

- `linux_amd64` — обычные x86-64 серверы Intel/AMD;
- `linux_arm64` — 64-битные ARM-серверы.

## Зависимости и сеть

Сервис автономный и stateless. Ему **не нужны** PostgreSQL, Redis, файловое
хранилище, очередь сообщений, control plane, credentials или секреты. Бинарник
собран с `CGO_ENABLED=0`, поэтому Go и системная libc во время работы также не
нужны. Docker не требуется.

На сервере нужны:

- 64-битный Linux соответствующей архитектуры с systemd;
- root/sudo для установки пользователя, unit-файла и бинарника;
- стандартные `bash`, `curl`, `sha256sum`, `install`, `getent`, `groupadd`,
  `useradd` и `systemctl`;
- публичный IPv4 и/или IPv6 либо корректный UDP port forwarding;
- входящий `3478/udp` в host firewall, cloud security group и внешнем firewall;
- DNS A/AAAA-запись публичного STUN-имени, если клиенты используют имя.

`9090/tcp` используется только локально на `127.0.0.1` для `/healthz`,
`/readyz` и `/metrics`. Открывать этот порт наружу не нужно. Постоянные
исходящие соединения сервису не требуются.

## Установка

Ниже `<version>` — точная версия из имени пакета, например `v1.0.10`.

1. Скопируйте архив и соседний файл `<archive>.sha256` на сервер.

2. Проверьте архив, распакуйте его и проверьте содержимое:

   ```sh
   sha256sum -c endlessnet-stun_<version>_linux_<arch>.tar.gz.sha256
   tar -xzf endlessnet-stun_<version>_linux_<arch>.tar.gz
   cd endlessnet-stun_<version>_linux_<arch>
   sha256sum -c SHA256SUMS
   ```

3. Один раз подготовьте системного пользователя, каталоги, конфигурацию и
   systemd unit:

   ```sh
   sudo ./scripts/install.sh
   ```

   Скрипт не запускает сервис до установки бинарника.

4. Отредактируйте конфигурацию:

   ```sh
   sudoedit /etc/endlessnet-stun/stun.env
   ```

   Для dual-stack сервера:

   ```dotenv
   ENDLESSNET_STUN_ADDRS=0.0.0.0:3478,[::]:3478
   ENDLESSNET_STUN_METRICS_ADDR=127.0.0.1:9090
   ENDLESSNET_STUN_RATE_LIMIT_PER_SECOND=20
   ENDLESSNET_STUN_RATE_LIMIT_BURST=40
   ENDLESSNET_STUN_LOG_LEVEL=info
   ```

   Если IPv6 на хосте выключен, оставьте только
   `ENDLESSNET_STUN_ADDRS=0.0.0.0:3478`: невозможность открыть любой из
   перечисленных listener-ов завершает запуск всего сервиса.

5. Проверьте конфигурацию до открытия сокетов:

   ```sh
   set -a
   . /etc/endlessnet-stun/stun.env
   set +a
   ./bin/endlessnet-stun --check-config
   ```

6. Установите точную версию, атомарно переключите symlink `current`, запустите
   unit и дождитесь readiness:

   ```sh
   VERSION=$(cat VERSION)
   EXPECTED_SHA256=$(awk '$2 == "bin/endlessnet-stun" { print $1 }' SHA256SUMS)
   sudo ./scripts/install-release.sh \
     "$VERSION" \
     "$PWD/bin/endlessnet-stun" \
     "$EXPECTED_SHA256"
   ```

   Установочный скрипт переносит бинарник из распакованного пакета в
   `/opt/endlessnet-stun/releases/$VERSION/`, поэтому его исчезновение из
   `bin/` после успешной команды ожидаемо.

## Проверка

Проверьте unit и локальные endpoints:

```sh
sudo systemctl status endlessnet-stun --no-pager
curl --fail http://127.0.0.1:9090/healthz
curl --fail http://127.0.0.1:9090/readyz
curl --fail http://127.0.0.1:9090/metrics
sudo journalctl -u endlessnet-stun -n 100 --no-pager
```

С другой машины выполните настоящий публичный STUN-запрос:

```sh
./bin/endlessnet-stun-smoke \
  --stun-addr stun.example.com:3478 \
  --timeout 5s
```

Локальный `/readyz` подтверждает только открытие UDP listener-а. Публичный
smoke-тест дополнительно проверяет DNS, NAT/port forwarding и firewall.

## Обновление

Распакуйте пакет новой точной версии, выполните `sha256sum -c SHA256SUMS`, затем
повторите шаги 5–6. Каталоги релизов неизменяемы, а
`/opt/endlessnet-stun/current` переключается атомарно. Если новая версия не
запустится или не пройдёт локальный readiness, `install-release.sh`
автоматически восстановит предыдущую версию.

## Ручной rollback

С машины оператора, у которой есть SSH-доступ к серверу, выполните:

```sh
SMOKE_BINARY="$PWD/bin/endlessnet-stun-smoke" \
DEPLOY_USER=<ssh-user> \
./scripts/rollback.sh \
  --target <server-host-or-ip> \
  --stun-addr stun.example.com:3478
```

Скрипт использует `sudo` на целевом сервере, возвращает ранее сохранённый релиз,
перезапускает только
`endlessnet-stun.service`, проверяет локальный readiness и выполняет публичный
STUN Binding smoke-тест.

## Runtime layout

```text
/etc/endlessnet-stun/stun.env
/etc/systemd/system/endlessnet-stun.service
/opt/endlessnet-stun/
├── current -> /opt/endlessnet-stun/releases/<version>
├── deployed-version
├── previous-release
└── releases/
    └── <version>/
        └── endlessnet-stun
```

Сервис работает от непривилегированного пользователя `endlessnet-stun`.
Логи — структурированный JSON в journald. Конфигурация не содержит секретов.
