# Jank Hunter CLI

`jankhunter` - это Go CLI для чтения `.jhlog`, анализа одного или нескольких логов, сравнения baseline/candidate и генерации standalone HTML-отчетов в технологичном темном стиле.

CLI не требует backend, базы данных или браузерных CDN. Отчет - обычный HTML-файл с CSS внутри.

## Возможности

- чтение бинарного `.jhlog`;
- чтение debug/export JSONL;
- генерация sample-лога;
- inspect-отчет по одному файлу или пулу файлов;
- compare-отчет baseline vs candidate с per-log drill-down внутри того же HTML;
- отдельный математический HTML-отчет рядом с inspect/compare и зеленая кнопка `λ Анализ` в основном отчете;
- математические разделы: timeline buckets, robust statistics, change points, autocorrelation/DFT, network loop detector, integral pain scores, Markov states, causal graph и compare deltas;
- справка по каждому математическому методу прямо в math HTML;
- streaming aggregation для `inspect`/`compare` без хранения всех событий в памяти;
- фильтры `--route`, `--screen`, `--owner`;
- owner-map import через `--owner-map path.json`;
- JSON output для `inspect` и `compare`;
- threshold config для CI regression gate;
- экспорт событий в JSONL;
- красивый standalone HTML без CDN: sticky navigation, gauges, animated bars, detailed tables;
- итоговый эвристический блок внизу HTML: status, findings и recommendations;
- сводка по HTTP, UI/FPS/jank, stalls, system context, memory, retained objects, counters, gauges;
- process breakdown из session metadata;
- cohort breakdown по app version/build/SDK/device/process/network;
- warnings, если baseline/candidate собраны на несопоставимых cohort;
- severity с учетом effect size, confidence и sample size;
- approximate confidence interval по ключевым deltas, когда есть выборка;
- retained-object section с top retained classes и age buckets;
- separate JankStats metrics section для `jankstats.*` counters/gauges;
- Top Owners по owner/class/stack hint.

## Сборка

```bash
cd cli
go test ./...
go build -o bin/jankhunter ./cmd/jankhunter
```

Release archives для macOS/Linux и checksum-файл:

```bash
cd cli
make release VERSION=0.1.0
```

Проверить embedded version:

```bash
make build VERSION=0.1.0
./bin/jankhunter version
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

Сгенерировать красивый standalone HTML-отчет:

```bash
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
```

Этот режим подходит для простого сценария "передать `.jhlog` и получить подробный отчет": в HTML будут overview, HTTP routes, UI smoothness, owner hotspots, memory/retained objects, custom counters/gauges, JankStats и context/cohort breakdown. Рядом автоматически создается `/tmp/report-math.html` с математическим анализом; основной отчет открывает его кнопкой `λ Анализ`.

Потенциально длинные таблицы маршрутов, экранов, owners, памяти, метрик и контекста спрятаны в раскрывающиеся блоки, чтобы большие логи не превращали первый экран отчета в бесконечную простыню.
Внизу отчета есть `Heuristic Verdict`: краткое итоговое заключение, список находок и рекомендации по следующим шагам.

Получить machine-readable JSON:

```bash
go run ./cmd/jankhunter inspect /tmp/sample.jhlog --json
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

HTML compare состоит из двух основных уровней:

- первый экран показывает scoreboard, regression matrix, cohort warnings и candidate summary;
- секция `Per-log Drill-down` раскрывает каждый baseline/candidate `.jhlog` отдельно с routes, screens, owners, memory и gauges; содержимое раскрытого лога ограничено по высоте и скроллится внутри карточки.

При `--out compare.html` рядом создается `compare-math.html`. В нем показаны baseline/candidate deltas для робастной статистики, точек изменения, сетевых циклов, интегральных оценок, Markov-переходов и графа причинности.

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
Process mix
App version mix
SDK mix
Device mix
Network mix
Cohort mix
```

Каждая delta получает:

- `confidence=low|medium|high`, рассчитанный из размера выборки;
- `sample=<n>`;
- approximate interval вроде `approx 120.00..150.00 ms, n=25`, когда это разумно;
- severity, которая учитывает effect size, confidence и sample size.

JSON:

```bash
go run ./cmd/jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --json > compare.json
```

CI gate через thresholds:

```json
{
  "max_severity": "medium",
  "min_confidence": "medium",
  "metrics": {
    "HTTP p95": {"max_regression_pct": 12},
    "UI jank rate": {"max_regression_abs": 1.5},
    "Retained objects": {"max_severity": "ok"}
  }
}
```

Запуск:

```bash
go run ./cmd/jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --thresholds thresholds.json \
  --out compare.html
```

Если gate падает, CLI возвращает exit code `1`, но HTML успевает сохраниться.

### export

Экспортировать события в JSONL:

```bash
go run ./cmd/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

## Математический HTML

Каждый `inspect --out report.html` создает пару файлов:

```text
report.html
report-math.html
```

Каждый `compare --out compare.html` создает:

```text
compare.html
compare-math.html
```

Основной HTML содержит кнопку `λ Анализ`. Math page полностью автономная, на русском языке и без CDN. Верхняя часть показывает сводку качества/сравнения и быстрые карточки всех разделов. Подробные таблицы закрыты по умолчанию и скроллятся внутри секций, поэтому большие логи остаются читаемыми.

Разделы math page:

- `Качество данных` или `Качество сравнения`: sample-size и честность сравнения;
- `Таймлайн сигналов`: временные бакеты и SVG sparklines;
- `Робастная статистика`: медиана, p95/p99, MAD, bootstrap-интервал и дельта Клиффа;
- `Точки изменения`: rolling median/MAD с ближайшим route/owner/screen/network context;
- `Периодические сигналы`: автокорреляция, DFT peaks, spectral entropy;
- `Сетевые циклы`: DNS/connect/retry/websocket-like loops, motif, confidence и burn score;
- `Интегральная оценка боли`: площади jank/latency/stall/memory/recovery debt;
- `Марковская модель состояний`: state sequence, transition matrix, sticky states и recovery probability;
- `Граф причинности`: Dijkstra paths, Floyd-Warshall all-pairs для компактного графа и owner blame score;
- `Справка по методам`: что измеряет каждый метод, как считается, как читать и какие есть ограничения.

## `.jhlog`

Формат бинарный:

```text
magic/version
record*

record:
  header: byte
    bits 0..3: event_type
    bit 4: flags follow
    bit 5: payload_len follows
    bits 6..7: timestamp_delta_ms encoding
  timestamp_delta_ms: 0 bytes, uint8, uint16le, or uvarint
  flags: uvarint, only when present
  payload_len: uvarint, only for variable-length payloads
  payload: event-specific bytes
```

Время хранится как монотонный delta-ms, а не как полная дата на каждое событие. Частые события обычно занимают 1-3 байта на timestamp часть. До явной фиксации формата `.jhlog` считается pre-release unstable: CLI читает только текущую схему.

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

## JankStats

Если Android runtime пишет `jankstats.*` metrics, CLI показывает отдельную секцию JankStats в text/HTML отчетах. Эти метрики не смешиваются с Choreographer `ui_window`: `ui_window` остается fallback FPS/jank signal, а JankStats показывает richer frame/state counters и duration gauges.

## System context

Context-события помогают понять условия замера:

```text
network kind
battery pct
available/total memory
low-memory flag
network metered/validated/VPN
uid rx/tx bytes
free/total app data storage
```

В `inspect` и `compare` эти данные показываются рядом с performance-метриками, чтобы отличать реальную регрессию от плохих условий устройства или сети. В верхней части HTML есть отдельная `Device Context` плашка с моделью устройства, Android/API/security patch, CPU ABI, батареей, сетью/VPN, свободной RAM и свободным storage на момент context sample.

## Cohorts

CLI группирует session/context metadata:

```text
app_versions: 1.4.0=8
sdks: api-35=5, api-34=3
devices: Pixel 8 / API 35=5
processes: main=8
network: wifi=20, cellular=4
cohorts: app=1.4.0 build=420 sdk=api-35 device=Pixel 8 / API 35 process=main network=wifi=120
```

`compare` добавляет warnings, если baseline/candidate собраны на разных app versions, SDK, devices, process mix, network mix или combined cohorts. Это не запрещает сравнение, но явно помечает риск ложного вывода.

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

HTML содержит:

- route details;
- screen details;
- Top Owners;
- retained-object drill-down;
- JankStats section;
- process/device/network/cohort breakdown;
- worst regression cards;
- compare warnings;
- зеленую кнопку `λ Анализ` и соседнюю math page при генерации через `--out`.

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

После inspect/compare должны появиться `/tmp/report-math.html` и `/tmp/compare-math.html`; в них должны быть `Математический анализ`, `Сетевые циклы`, `Граф причинности` и `Справка по методам`.

## Ограничения статистики

CLI работает локально и streaming-first. Confidence interval сейчас approximate и основан на агрегатах, а не на полноценном bootstrap по всем raw events. Для CI gate это достаточно, чтобы не игнорировать sample size, но окончательные релизные выводы всё равно стоит подтверждать на сопоставимых cohorts.
