# Jank Hunter CLI

`jankhunter` - это Go CLI для чтения `.jhlog`, анализа одного или нескольких логов, сравнения baseline/candidate и генерации standalone HTML-отчетов.

CLI не требует backend, базы данных или браузерных CDN. Отчет - обычный HTML-файл с CSS внутри.

## Возможности

- чтение бинарного `.jhlog`;
- чтение debug/export JSONL;
- генерация sample-лога;
- inspect-отчет по одному файлу или пулу файлов;
- compare-отчет baseline vs candidate;
- streaming aggregation для `inspect`/`compare` без хранения всех событий в памяти;
- фильтры `--route`, `--screen`, `--owner`;
- owner-map import через `--owner-map path.json`;
- экспорт событий в JSONL;
- сводка по HTTP, UI/FPS/jank, stalls, system context, memory, retained objects, counters, gauges;
- process breakdown из session metadata;
- retained-object section с top retained classes и age buckets;
- top suspects по owner/class/stack hint.

## Сборка

```bash
cd cli
go test ./...
go build -o bin/jankhunter ./cmd/jankhunter
```

Запуск без сборки:

```bash
go run ./cmd/jankhunter help
```

## Команды

### sample

Создать демонстрационный `.jhlog`:

```bash
go run ./cmd/jankhunter sample --out /tmp/sample.jhlog
```

### inspect

Разобрать один лог и напечатать краткую сводку:

```bash
go run ./cmd/jankhunter inspect /tmp/sample.jhlog
```

Сгенерировать HTML-отчет:

```bash
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
```

Можно передать несколько файлов:

```bash
go run ./cmd/jankhunter inspect logs/*.jhlog --out report.html
```

Фильтры:

```bash
go run ./cmd/jankhunter inspect logs/*.jhlog --route /feed --screen Feed --owner FeedRepository
```

С owner-map из Gradle plugin:

```bash
go run ./cmd/jankhunter inspect logs/*.jhlog \
  --owner-map ../android/sample-app/build/generated/jankhunter/debug/owner-map.json \
  --out report.html
```

### compare

Сравнить baseline и candidate:

```bash
go run ./cmd/jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --owner-map owner-map.json \
  --out compare.html \
  --owner FeedRepository
```

CLI покажет deltas:

```text
HTTP p95
HTTP failures
UI jank rate
UI avg FPS
Main-thread stall max
Max PSS
Min available memory
UID RX max
UID TX max
Retained objects
```

Каждая delta получает `confidence=low|medium|high`, рассчитанный из размера выборки. Это не заменяет полноценную статистику, но не дает отчету притворяться уверенным на слишком маленьком наборе логов.

### export

Экспортировать события в JSONL:

```bash
go run ./cmd/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

## `.jhlog`

Формат бинарный:

```text
magic/version
record*

record:
  event_type: uvarint
  timestamp_delta_ms: uvarint
  flags: uvarint
  payload_len: uvarint
  payload: event-specific bytes
```

Строки пишутся через dictionary records:

```text
id -> "FeedRepository.refresh"
id -> "GET /feed"
id -> "FeedScreen"
```

Runtime пишет короткие ID, CLI раскрывает их в имена.
Новые session events также могут содержать `process_id`; старые `.jhlog` без process metadata читаются как `unknown`.

## Owner map

Gradle plugin пишет seed-файл:

```text
android/<app>/build/generated/jankhunter/<variant>/owner-map.json
```

CLI принимает его в `inspect` и `compare`:

```bash
go run ./cmd/jankhunter inspect logs/*.jhlog --owner-map owner-map.json
go run ./cmd/jankhunter compare --baseline old/*.jhlog --candidate new/*.jhlog --owner-map owner-map.json
```

Поддерживаются две формы:

```json
{
  "owners": {
    "FeedRepository.refresh": "Feed team / refresh",
    "com.example.FeedPresenter.bind#abcd1234": "FeedPresenter.bind"
  }
}
```

и:

```json
{
  "entries": [
    {"id": "abcd1234", "owner": "FeedPresenter.bind"}
  ]
}
```

Если owner-map не содержит matching entry, CLI показывает owner label из `.jhlog` как есть.

## UI/FPS

SDK пишет UI-window события:

```text
screen_id
window_ms
frame_count
jank_count
p50_ms
p95_ms
p99_ms
```

CLI считает:

```text
avg_fps = frame_count * 1000 / window_ms
jank_rate = jank_count / frame_count
```

В HTML-отчете есть:

- общий Avg FPS;
- jank rate;
- встроенные chart-блоки без CDN;
- top janky screens;
- Avg FPS и Min FPS по каждому screen;
- p95/p99 frame duration.

## System context

Context-события помогают понять условия замера:

```text
network kind
battery pct
available memory
low-memory flag
network metered/validated
uid rx/tx bytes
```

В `inspect` и `compare` эти данные показываются рядом с performance-метриками, чтобы отличать реальную регрессию от плохих условий устройства или сети.

## Process breakdown

Если runtime пишет process metadata, `inspect` показывает session count по process:

```text
processes: main=1, remote=1
```

HTML-отчеты содержат отдельную process table. `compare` добавляет delta “Process mix”, чтобы не сравнивать baseline только из main process с candidate, где появились remote-process логи, без явного сигнала.

## Counters и gauges

Counters суммируются:

```text
logs.warn.count
jankhunter.events_dropped.count
```

Gauges усредняются и показывают детали:

```text
ui.fps_x100
memory.heap_pressure
```

## HTML-отчеты

`inspect` генерирует отчет по текущей ситуации.

`compare` генерирует отчет по регрессиям между baseline и candidate.

Отчет самодостаточный:

- HTML;
- встроенный CSS;
- без CDN;
- можно отправить в артефакты CI или открыть локально.

## Проверка MVP

```bash
cd cli
go test ./...
go run ./cmd/jankhunter sample --out /tmp/sample.jhlog
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
go run ./cmd/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/compare.html
go run ./cmd/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

## Дальнейшее развитие

- device tier / cohort normalization;
- более строгая статистическая модель с confidence intervals;
- интерактивные drill-down графики без CDN.
