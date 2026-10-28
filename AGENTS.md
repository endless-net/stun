# Правила работы: stun

Репозиторий публичный и использует автономный локальный профиль по STD-030,
STD-035 и STD-036. Этот профиль заменяет прежний private include: чтение
Service Kit или приватного workspace, их credentials, код, workflows и образы
не являются обязательной зависимостью. Приватный профиль не копируется сюда.

- Перед изменениями прочитать существующие документы затронутого контракта.
  Зафиксировать revision и dirty state прочитанных источников; недоступность
  необходимого источника назвать явно.
- Выполнять только запрошенную работу в этом репозитории. Сохранять чужие локальные
  изменения; соседний checkout или упоминание consumer не разрешает его менять.
  Commit/push, PR, выпуск, deployment и реальные инфраструктурные операции требуют
  соответствующего запроса. Skills без явного разрешения не изменять.
- Spec Kit устанавливается и запускается только в workspace. В этом репозитории
  не создавать его конфигурацию, skills, команды или конституцию. При работе через
  workspace использовать его согласованные spec/plan/tasks; автономная работа
  опирается на этот AGENTS.md. Предложение и успешная сборка не означают принятие
  продукта или разрешение перейти к runtime.
- Схема, генерация и SDK принадлежат producer (STD-006/007). Consumer не создаёт
  ручную копию доменной модели. API/wire/runtime versions и migration status
  фиксировать отдельно; credentials не помещать в Git, логи или generated files.
- Для public CI использовать GitHub-hosted runners и публичные локальные configs
  (STD-015/029/033/035). Общие Bash/scripts/baseline checks выполняются в Ubuntu
  26.04; platform-specific проверки отдельно указывают OS и точный scope.
- При отказе необходимого инструмента или проверке с skip фиксировать not-verified,
  не pass. Для изменения кода выполнять применимые обязательные проверки всего
  затронутого модуля, а не только изменённых строк. Для документации проверить
  локальные ссылки и `git diff --check`.

## Сохранённые правила локального профиля

Неизвестные policy/timeout/limit values не выдумываются. Наблюдаемые результаты,
запрещённые исходы и применимые проверки связываются с постановкой задачи.

- STD-016/018/020/023/024: authority определяется настоящим caller/resource/action,
  dependency failure не расширяет доступ; bounded deadline/cancellation/retry,
  immutable replay inputs, expiry и current-state checks у соответствующего owner.
  Shape/serialization fixtures не доказывают runtime authentication или durable replay.
- STD-034: локальная работа ограничена запрошенным owner scope; consumer references
  не разрешают менять чужой checkout. Host lifecycle/provision/deployment/
  activation/rollback относятся к внешнему платформенному контуру вне активного
  состава. Эти функции не переходят сервисам или Service Kit; место и owner
  требуют отдельного решения COMPOSITION-Q-01. Изолированный test fixture
  не получает production authority.
- Public consumer может получать private producer API по закреплённой версии с
  read-only credentials при сборке. Это не разрешает Service Kit dependency;
  credentials не попадают в Git/logs/artifacts и недоверенные external PR, а
  отсутствие необходимого доступа завершает fetch ошибкой без fallback.
- Применимость и exact revision источников и результатов проверок фиксируются;
  proposed решения не становятся accepted из-за документа или успешной сборки.

Локальные особенности указывают причину, область и заменяемое положение.
Наличие документов не расширяет разрешённые записи или публикации.

## Проверки владельца

Корневой `go.mod` содержит самостоятельный runtime `cmd/stun/main.go` и
`internal/server`: bounded UDP Binding, lifecycle, limiter и diagnostics. Публичный lint config — `.golangci-lint.yaml`.
Из корня в Ubuntu 26.04 выполнять read-only базовый набор:

```bash
goimports -l .
go vet ./...
golangci-lint run --config .golangci-lint.yaml ./... --timeout 1m
go test -short ./...
govulncheck ./...
```

Непустой вывод `goimports -l` означает ошибку форматирования; исправления выполняются
локально и просматриваются отдельно. Пустой main не получает бессодержательный unit
test ради формального покрытия. Сборка Linux amd64 выполняется отдельно через
`python3 -B scripts/ci.py build` после базовых проверок.

[Локальный wire profile и vectors](api/README.md) задают стандартный UDP Binding.
Из корня репозитория выполнить:

```bash
python3 -B api/test_wire_vectors.py
```

Проверяются synthetic IPv4/IPv6/negative codec cases без UDP server. Каталог `api/`
остаётся wire/JSON-контрактом и offline fixtures: отдельного `api/go.mod` и
Protobuf RPC в нём нет, Buf-проверка неприменима. Network IPv6, retransmit/rate
limits и production host acceptance не проверяются базовым CI и относятся к
отдельному этапу. STUN не TURN, не payload forwarding и не
Account authority; публичный Binding не получает обязательный private SPIFFE
include. Host provisioning/deploy/activation относятся к внешнему платформенному
контуру вне активного состава, без передачи STUN или Service Kit; место и owner
требуют отдельного решения COMPOSITION-Q-01.

## MVP v0 и цикл 002

Новая v0-линейка НЕ поддерживает backward compatibility до отдельного объявления
стабильного major новой линейки, включая patch. Изменения — hard cutover, без
shims, dual API и fallback ради прошлых версий. Сохраняемые данные обновляются
прямыми миграциями; скрытый reset запрещён. Runtime и api/v0.* версионируются
независимо. Consumer закрепляет точную версию producer API; `@latest`, first-party
local replace и обход pin через GOWORK запрещены.

Профиль .ci/profile.json явно перечисляет все собственные модули и outputs.
Базовый CI: format/lint, go vet, go test -short и govulncheck для Go, применимые
Dart/JSON unit/static checks. Integration/E2E и тесты прежних версий в обязательный
MVP gate не входят. Отсутствие unit cases у пустого main фиксируется без выдуманного
покрытия. Runtime008 реализует стандартный UDP Binding, bounded lifecycle и diagnostics;
базовые unit/static проверки не заменяют отдельные real-socket/load suites.

Автономные команды: python3 -B scripts/ci.py check и build; unit tooling — scripts/test_ci.py.
Kit не загружается, собственные публичные lint configs остаются у владельца.
Release допускается только по push обычного v0-тега после exact commit checks и
сборки. API-тег выбирает только source/SDK, runtime-тег — runtime outputs.
Spec Kit и общая конституция находятся только в workspace.


## Runtime008 — дополнительные команды

Текущий runtime следует малому профилю в README.md. При изменении server code
выполнить meaningful unit tests и обязательный baseline выше. Отдельная приёмка:
go test -race -tags=integration ./internal/server -timeout=40s;
go build -o /tmp/stun-runtime ./cmd/stun;
STUN_BINARY=/tmp/stun-runtime python3 -B test/acceptance/process_test.py;
bash test/acceptance/run.sh (полный860s профиль, изолированный network namespace).
Эти suites не входят в обязательный short CI и не подтверждают внешнюю сеть.
Новый runtime tag разрешён только после successful main CI exact commit;
main ancestry и CI проверяются publisher. Runtime/API и внешняя системная
приёмка сохраняют раздельный статус.
