# Jank Hunter CLI

`jankhunter` - это Go CLI для чтения `.jhlog`, анализа одного или нескольких логов, сравнения baseline/candidate и генерации standalone HTML-отчетов.

CLI не требует backend, базы данных или браузерных CDN. Отчет - обычный HTML-файл с CSS внутри.

## Возможности

- чтение бинарного `.jhlog`;
- чтение debug/export JSONL;
- генерация sample-лога;
- inspect-отчет по одному файлу или пулу файлов;
- compare-отчет baseline vs candidate;
- экспорт событий в JSONL;
- сводка по HTTP, UI/FPS/jank, stalls, memory, retained objects, counters, gauges;
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

### compare

Сравнить baseline и candidate:

```bash
go run ./cmd/jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --out compare.html
```

CLI покажет deltas:

```text
HTTP p95
HTTP failures
UI jank rate
UI avg FPS
Main-thread stall max
Max PSS
Retained objects
```

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
- top janky screens;
- Avg FPS и Min FPS по каждому screen;
- p95/p99 frame duration.

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

- streaming aggregation без хранения всех событий в памяти;
- SVG/canvas-графики внутри HTML;
- фильтры по route/screen/owner/device tier;
- owner map import из Gradle plugin;
- чтение частично поврежденных `.jhlog`;
- severity model с confidence и sample size.
