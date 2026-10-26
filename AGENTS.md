# Правила работы: stun

Репозиторий публичный и использует автономный локальный профиль по STD-030,
STD-035 и STD-036. Этот профиль заменяет прежний private include: чтение
Service Kit или приватного workspace, их credentials, код, workflows и образы
не являются обязательной зависимостью. Приватный профиль не копируется сюда.

- Перед изменениями прочитать [локальную конституцию](.specify/memory/constitution.md)
  и существующие документы затронутого контракта. Зафиксировать revision и dirty
  state прочитанных источников; недоступность необходимого источника назвать явно.
- Выполнять только запрошенную работу в этом репозитории. Сохранять чужие локальные
  изменения; соседний checkout или упоминание consumer не разрешает его менять.
  Commit/push, PR, выпуск, deployment и реальные инфраструктурные операции требуют
  соответствующего запроса. Skills без явного разрешения не изменять.
- Spec Kit запускать только по явному запросу пользователя; запрошенный этап
  не запускает следующие автоматически. Предложение и успешная сборка не означают
  принятие продукта или разрешение перейти к runtime.
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

## Проверки владельца

[Локальный wire profile и vectors](api/README.md) задают стандартный UDP Binding.
Из корня репозитория выполнить:

```bash
python3 -B api/test_wire_vectors.py
```

Проверяются synthetic IPv4/IPv6/negative codec cases без UDP server; network IPv6,
retransmit/rate limits и production host acceptance требуют отдельных проверок.
В текущем checkout нет Go module или Protobuf RPC, Go/Buf checks неприменимы;
не добавлять их ради формальности. STUN не TURN, не payload forwarding и не
Account authority; публичный Binding не получает обязательный private SPIFFE
include. Host provisioning/deploy/activation относятся к внешнему платформенному
контуру вне активного состава, без передачи STUN или Service Kit; место и owner
требуют отдельного решения COMPOSITION-Q-01.
