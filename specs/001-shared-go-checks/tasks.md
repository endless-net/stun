# Tasks: stun

**Статус:** черновик локального планирования; реализация `not-verified`.

Владелец: `services/stun`. Общая фича: [specs/002-shared-go-checks](https://github.com/endless-net/workspace/blob/0586e302ac17214698f95d95cc04d3a63ae4a470/specs/002-shared-go-checks/spec.md).
Создание этих файлов не разрешает обход локальных правил, изменение чужого
кода, публикацию, запуск агентов или чатов. Перед кодом уточнить локальный
план, выполнить analyze/Guard и разрешить зависимости общей функции.

Источник T IDs: [root tasks](https://github.com/endless-net/workspace/blob/0586e302ac17214698f95d95cc04d3a63ae4a470/specs/002-shared-go-checks/tasks.md). Общие задачи workspace сюда не перенесены.

- [ ] T025 [P] [US2] (C14, services/stun, FR-003/004/009) В specs/001-shared-go-checks/ и owner check profile подготовить common baseline; активировать scripts/check.sh/.github/workflows/ci.yml и exact проверки при появлении module; до того pending.

- [ ] T029 [US3] (C14, services/stun, FR-005/008/010) В owner specs/001-shared-go-checks/plan.md и .github/workflows/ci.yml назначить обязательные service/platform suites и полный aggregate; при отсутствии применимого extension объяснить N/A, заготовки pending. Доменные tests и wide System Tests не перемещать в Kit.

Перед выполнением уточнить local plan и проверки; завершение и evidence отмечает owner.
