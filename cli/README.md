# Jank Hunter CLI

`jankhunter` - консольная утилита для `.jhlog` файлов. Она читает логи с Android-устройства и делает отчеты: одиночный прогон через `inspect`, сравнение двух прогонов через `compare`, экспорт событий через `export`.

CLI работает локально. Никакой backend не нужен: на выходе обычный HTML-файл, который можно открыть в браузере или положить в CI artifacts.

## Установка

Самый простой вариант:

```bash
make build
```

Готовый бинарник будет здесь:

```text
bin/jankhunter
```

Если Go не установлен, Makefile сам скачает Go в локальную папку:

```text
cli/.tools/go
```

Системный Go при этом не трогается.

Поставить команду в систему:

```bash
make install
```

По умолчанию установка идет в `/usr/local/bin`. Если нужны права попроще:

```bash
make install PREFIX="$HOME/.local"
```

Проверка:

```bash
jankhunter version
```

Сборка под другую платформу:

```bash
make build BUILD_OS=linux BUILD_ARCH=amd64 OUT=bin/jankhunter-linux-amd64
make build BUILD_OS=darwin BUILD_ARCH=arm64 OUT=bin/jankhunter-darwin-arm64
```

Архивы для нескольких платформ:

```bash
make release VERSION=1.0.0
```

## Быстрая проверка

```bash
make build
./bin/jankhunter sample --out /tmp/sample.jhlog
./bin/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
./bin/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/compare.html
```

После этого появятся:

```text
/tmp/report.html
/tmp/report-math.html
/tmp/report-leaks.html
/tmp/report-influence.html
/tmp/compare.html
/tmp/compare-math.html
/tmp/compare-leaks.html
/tmp/compare-influence.html
```

Для демо и ревью с командой добавьте `--presentation`: основной отчет, математическая страница и граф влияния получают presentation mode с более крупными акцентами и печатным CSS.

```bash
jankhunter inspect logs/*.jhlog --presentation --out report.html
jankhunter compare --baseline old/*.jhlog --candidate new/*.jhlog --presentation --out compare.html
```

Основной отчет открывается как обычный HTML. Математический отчет открывается из зеленой кнопки `λ Анализ`, граф влияния кода - из кнопки `Граф влияния`, если в логах есть owner/flow-сигналы.

## inspect

`inspect` нужен, когда есть один лог или пачка логов и нужно понять, что происходило в прогоне.

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Что будет в отчете:

- быстрый верхний срез: HTTP p95, UI, FPS, память, трафик;
- контекст устройства: Android, ABI, батарея, сеть/VPN, RAM, storage, рут-доступ;
- маршруты HTTP;
- экраны и подтормаживания UI;
- источники работ: owners/classes/stack hints;
- память и retained objects;
- counters/gauges;
- когорты;
- эвристический итог внизу;
- отдельная страница математического анализа;
- отдельный граф влияния кода, где классы связаны с runtime-проблемами и build-time ASM-графом.

Несколько логов можно передавать сразу:

```bash
jankhunter inspect logs/main/*.jhlog logs/remote/*.jhlog --out report.html
```

Фильтры:

```bash
jankhunter inspect logs/*.jhlog \
  --route /feed \
  --screen Feed \
  --owner FeedRepository \
  --out feed-report.html
```

JSON вместо HTML:

```bash
jankhunter inspect logs/*.jhlog --json > inspect.json
```

Owner map и class graph от Android Gradle plugin:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map ../android/sample-app/build/generated/jankhunter/debug/owner-map.json \
  --class-graph ../android/sample-app/build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics ../android/sample-app/build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --out report.html
```

`--owner-map` читает JSONL v2 от Gradle plugin и раскрывает generated owner hash-и в человекочитаемые `class.method`, `--class-graph` добавляет статические связи, горячие пути и method-level hotspots, а `--instrumentation-diagnostics` открывает отдельный ASM-отчет с matched hooks и disabled/unsupported decisions. Рядом с `report.html` появятся `report-influence.html` и `report-diagnostics.html`.

Heap evidence для точных цепочек утечек:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-dump dumps/checkout.hprof \
  --out report.html
```

`--heap-dump` строит путь от GC root до retained-класса из runtime-событий `watchObject`/`watchActivity`, показывает категорию GC root, пользовательский класс-держатель, поле ссылки, retained size, мини-дерево доминирования и альтернативные цепочки удержания. Если дамп слишком большой или цепочка не найдена, отчет остается в легком режиме с оценкой по runtime/PSS. При `--out report.html` рядом создается `report-leaks.html`: без heap evidence это junior-friendly light report, а с heap evidence - Leak Explorer с интерактивным графом цепочки удержания, чеклистом расследования, примерами фикса и шагами верификации.

Как читать leak report:

- `Light mode` означает, что SDK увидел retained object, но точного GC root пока нет. Смотрите `holder`, `screen`, `flow`, `step`, возраст и рекомендации.
- `Heap mode` означает, что CLI связал retained object с HPROF/evidence и показывает `GC root -> holder field -> retained object`.
- `chain_fingerprint` используется для стабильного сравнения версий: переименование holder-а не ломает match, если нормализованная цепочка та же.
- `alternative_paths` показывает дополнительные цепочки, через которые объект тоже достижим. Это важно для сложных утечек, где фикс одной ссылки не освобождает объект.

Можно передать уже подготовленный JSON evidence вместо HPROF:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-evidence heap-evidence.json \
  --out report.html
```

Минимальная форма JSON:

```json
{
  "leaks": [{
    "class_name": "com.app.checkout.CheckoutActivity",
    "holder": "com.app.checkout.CheckoutPresenter",
    "holder_field": "com.app.checkout.CheckoutPresenter.activity",
    "gc_root": "sticky class",
    "gc_root_category": "class/static",
    "chain_fingerprint": "com.app.checkout.CheckoutActivity|class/static|com.app.checkout.CheckoutPresenter|static activity",
    "retained_size_kb": 8192,
    "retained_object_count": 4,
    "reference_path": [
      {"class_name": "GC root: sticky class", "kind": "gc_root"},
      {"class_name": "com.app.checkout.CheckoutPresenter", "kind": "root_object"},
      {"class_name": "com.app.checkout.CheckoutActivity", "field_name": "activity", "kind": "field"}
    ],
    "alternative_paths": [
      [
        {"class_name": "GC root: thread object", "kind": "gc_root"},
        {"class_name": "com.app.checkout.Worker", "kind": "root_object"},
        {"class_name": "com.app.checkout.CheckoutActivity", "field_name": "callback", "kind": "field"}
      ]
    ]
  }]
}
```

## compare

`compare` нужен, когда есть база и кандидат. Например: сборка до изменения и сборка после изменения.

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --out compare.html
```

В compare-отчете есть:

- сводная панель базы и кандидата;
- матрица регрессий по категориям: сеть, UI, память, контекст;
- подсказки по метрикам;
- блок `Где изменилось` с парными таблицами маршрутов, экранов и источников;
- проверка когорт, чтобы не сравнивать разные устройства/SDK/сети как будто это один и тот же прогон;
- детали каждого лога внутри раскрывающихся карточек;
- эвристический итог;
- отдельный `compare-math.html`;
- отдельный `compare-leaks.html` со статусами `new`, `worse`, `same`, `better`, `resolved`;
- отдельный `compare-influence.html` для графа влияния кандидата.

С owner-map:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --owner-map owner-map.json \
  --class-graph class-graph.jsonl \
  --out compare.html
```

JSON:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --json > compare.json
```

Heap evidence для сравнения передается отдельно для базы и кандидата:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --baseline-heap-dump old/heap.hprof \
  --candidate-heap-dump new/heap.hprof \
  --out compare.html
```

## CI gate

Для CI можно задать пороги:

```json
{
  "max_severity": "medium",
  "min_confidence": "medium",
  "metrics": {
    "HTTP p95": {"max_regression_pct": 12},
    "UI jank rate": {"max_regression_abs": 1.5},
    "Retained objects": {"max_severity": "ok"}
  },
  "leaks": {
    "max_candidate_total": 10,
    "max_new": 0,
    "max_worse": 0,
    "max_high": 0,
    "max_runtime_only": 5,
    "fail_on_new": true,
    "fail_on_worse": true,
    "fail_on_new_high": true,
    "require_heap_for_high": true
  }
}
```

Поля `max_new: 0` и `max_worse: 0` сами по себе не включают проверку "ноль новых/ухудшенных", потому что ноль используется как значение по умолчанию. Для строгого режима используйте `fail_on_new` и `fail_on_worse`.

Запуск:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --thresholds thresholds.json \
  --out compare.html
```

Если gate падает, команда возвращает exit code `1`. HTML при этом сохраняется, чтобы было что открыть и посмотреть.

## export

Экспорт событий в JSONL:

```bash
jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

Это удобно, если нужно быстро проверить сырые события или скормить их другому инструменту.

## problems

Экспорт реестра проблем кода для обсуждения с командой:

```bash
jankhunter problems logs/*.jhlog --out problems.csv
jankhunter problems logs/*.jhlog --format json --out problems.json
```

CSV содержит drill-down строки `класс -> метод -> экран/флоу/шаг/маршрут -> доказательства -> рекомендация`. В реестре есть категории `ANR-risk`, `OOM-risk`, `GC pressure`, `duplicate network`, `lifecycle leak`, `log spam`, `main-thread IO` и базовые группы отчета.

Для отдельного leak-реестра:

```bash
jankhunter problems logs/*.jhlog --dataset leaks --out leaks.csv
jankhunter problems logs/*.jhlog --dataset leaks --format json --out leaks.json
```

`leaks.csv` включает `gc_root_category`, `chain_fingerprint`, `alternative_paths`, `investigation_steps`, `fix_examples` и `verification_steps`, поэтому его можно прикладывать к PR или CI artifacts без HTML.

## Как забрать логи с Android

По умолчанию runtime пишет:

```text
context.filesDir/jankhunter/session-<process>-<timestamp>-<segment>.jhlog
```

Через adb:

```bash
APP_ID=com.myapp
mkdir -p logs

adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs

jankhunter inspect logs/*.jhlog --out report.html
```

Для debug/QA сценария с heap dump можно использовать готовый helper:

```bash
cli/scripts/collect-android-leak-report.sh \
  --package com.myapp \
  --out /tmp/jankhunter-leaks \
  --cli ./cli/bin/jankhunter
```

Скрипт забирает `files/jankhunter` через `adb run-as`, находит `.jhlog` и `.hprof`, запускает `inspect` и кладет рядом `report.html`, `report-leaks.html`, `report-math.html` и дополнительные страницы.

Если лог получили через FileProvider/share sheet, просто положите файл в папку и передайте его CLI:

```bash
jankhunter inspect ~/Downloads/*.jhlog --out ~/Downloads/jankhunter-report.html
```

## Математический отчет

Для `inspect --out report.html` рядом создается:

```text
report-math.html
report-leaks.html
report-influence.html
report-diagnostics.html
```

Для `compare --out compare.html`:

```text
compare-math.html
compare-leaks.html
compare-influence.html
compare-diagnostics.html
```

Там лежат более тяжелые методы:

- временные бакеты и sparklines;
- робастная статистика;
- точки изменения;
- автокорреляция и DFT peaks;
- сетевые циклы;
- интегральные оценки боли;
- Марковская модель состояний;
- граф причинности;
- справка по каждому методу.

Основная идея такая: сначала смотрим обычный отчет, потом открываем `λ Анализ`, если нужно понять глубже, откуда взялась проблема.

## Граф влияния кода

`report-influence.html` и `compare-influence.html` показывают классы, которые чаще всего связаны с проблемами: паузами главного потока, сетевыми хвостами, UI-подтормаживаниями, ростом памяти, удержанными объектами и спамом логами.

Лучший режим - передать и `.jhlog`, и build-time class graph:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map build/generated/jankhunter/debug/owner-map.json \
  --class-graph build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --out report.html
```

Если `--class-graph` не передан, CLI все равно покажет runtime-подозреваемых и runtime-вызовы из `.jhlog`, но без build-time ребер между классами. Статический узел без runtime-доказательств означает не “виноват”, а “связан с кодом, но в этом прогоне не проявился”.

## `.jhlog` коротко

Формат бинарный и компактный:

- timestamp хранится как delta-ms, а не полной датой в каждом событии;
- строки лежат в dictionary records;
- события дальше используют короткие ID;
- boolean-сигналы пишутся в flags/bitmask;
- контекст `screen/owner/flow/step` пишется только когда поле реально есть;
- runtime call graph хранится как агрегаты `caller_id -> callee_id`, а не как сырые вызовы;
- `context.battery_temp_deci_c` хранится signed zigzag-varint, поэтому отрицательная температура батареи остается корректной величиной;
- metric events для gauges несут `value/count/sum/max/mode`, чтобы CLI мог объединять Android-side flush-окна взвешенно, а не усреднять уже усредненные значения;
- числовые строки и даты в словаре BCD-пакуются, когда это короче обычной строки;
- Android runtime по умолчанию сжимает тело файла gzip-потоком после magic-заголовка, CLI читает и сжатый, и старый несжатый body;
- события Handler/OkHttp не пишут повторяющиеся строки: owner/route/flow остаются в словаре, а события ссылаются на короткие ID.

Gauge aggregation mode хранится рядом с metric event:

- `AVERAGE` - обычные числовые gauge-метрики; CLI складывает `sum/count` и показывает weighted average плюс max;
- `LAST` - идентификаторы и последние уровни (`*.last_id`, `*.last_level`, `*.core_count`, `*.max_kb`), где среднее не имеет смысла;
- `STATE` - enum/state метрики вроде battery status, plugged, health, thermal status, process exit reason/importance;
- `BOOLEAN_RATE` - boolean gauge-метрики; в отчете значение означает процент `true` за окно.

Пользовательские negative gauges сейчас не пишутся как signed metric values: runtime считает их ошибочным input и увеличивает `jankhunter.metric.invalid_negative.gauge.count`. Если нужна отрицательная физическая величина, она должна иметь отдельный typed event или signed context field, как `battery_temp_deci_c`.

До фиксации первой стабильной версии формат можно ломать и улучшать. Сейчас CLI читает текущую схему `FormatVersion=5`.

## Проверки

```bash
make test
make build
./bin/jankhunter sample --out /tmp/sample.jhlog
./bin/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
./bin/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

Для чистки сборочных артефактов:

```bash
make clean
```
