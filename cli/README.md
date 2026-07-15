# Командная Утилита Jank Hunter

`jankhunter` это локальная командная утилита для `.jhlog` файлов. Она читает логи с Android-устройства и создаёт отчёты по одному прогону, сравнению двух прогонов, утечкам памяти, математическому разбору, графу влияния кода и диагностике ASM-внедрения.

Сервер не нужен. На выходе обычные HTML, CSV, JSON или JSONL файлы, которые можно открыть в браузере, приложить к задаче или сохранить в файлах проверки сборки.

## Установка

Сборка:

```bash
make build
```

Готовый файл:

```text
bin/jankhunter
```

Если Go не найден, Makefile скачает Go `1.22.12` в локальный каталог:

```text
cli/.tools/go
```

Установка команды:

```bash
make install
make install PREFIX="$HOME/.local"
```

Проверка:

```bash
jankhunter version
```

Текущие значения:

```text
Jank Hunter CLI 1.0.2
.jhlog format 9
```

Сборка под другую систему:

```bash
make build BUILD_OS=linux BUILD_ARCH=amd64 OUT=bin/jankhunter-linux-amd64
make build BUILD_OS=darwin BUILD_ARCH=arm64 OUT=bin/jankhunter-darwin-arm64
```

Архивы по списку `PLATFORMS` из Makefile:

```bash
make release VERSION=1.0.2
```

## Быстрая Проверка

```bash
make build
./bin/jankhunter sample --out /tmp/baseline.jhlog
./bin/jankhunter sample --out /tmp/candidate.jhlog
./bin/jankhunter inspect /tmp/baseline.jhlog --out /tmp/report.html
./bin/jankhunter compare --baseline /tmp/baseline.jhlog --candidate /tmp/candidate.jhlog --out /tmp/compare.html
./bin/jankhunter size /tmp/baseline.jhlog
```

После `inspect` создаётся один самодостаточный отчёт:

```text
/tmp/report.html
```

После `compare` — также один файл:

```text
/tmp/compare.html
```

Обзор, математический анализ, утечки и граф влияния доступны во вкладках внутри HTML.
`--instrumentation-diagnostics` добавляет вкладку «ASM диагностика», а `--di-catalog` —
вкладку «DI-каталог». Внешние ресурсы и соседние HTML-файлы отчёту не нужны.

Для доклада или обсуждения с командой можно включить более крупные акценты:

```bash
jankhunter inspect logs/*.jhlog --presentation --out report.html
jankhunter compare --baseline old/*.jhlog --candidate new/*.jhlog --presentation --out compare.html
```

Фон отчетов по умолчанию статичный. Если нужен декоративный сканирующий фон, включите его явно:

```bash
jankhunter inspect logs/*.jhlog --animated-background --out report.html
jankhunter compare --baseline old/*.jhlog --candidate new/*.jhlog --animated-background --out compare.html
```

## Команды

Краткая карта:

- `sample`: создаёт пример `.jhlog`.
- `inspect`: разбирает один или несколько логов.
- `compare`: сравнивает базовый и проверяемый прогоны.
- `problems`: выгружает проблемные места в CSV или JSON.
- `scorecard`: строит JSON-оценку качества данных и готовности сравнения.
- `export`: пишет сырые события в JSONL.
- `size`: показывает профиль размера логов.
- `version`: печатает версию утилиты и формат `.jhlog`.

Справка:

```bash
jankhunter help
```

## Inspect

Один или несколько логов:

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Что видно в отчёте:

- верхний срез: сеть, плавность, частота кадров, память, паузы главного потока и трафик;
- сведения об устройстве: Android, API, патч безопасности, ABI, сеть, VPN, батарея, память, хранилище и root-доступ;
- маршруты HTTP и WebSocket-сигналы;
- экраны и окна кадров;
- источники работ, классы и подсказки по стеку;
- удержанные объекты и отчёт утечек;
- пользовательские счётчики и числовые метрики;
- проблемные окна и спам логами;
- граф влияния кода;
- математический разбор;
- итоговая эвристика.

Фильтры:

```bash
jankhunter inspect logs/*.jhlog \
  --route /feed \
  --screen Feed \
  --owner FeedRepository \
  --class CheckoutPresenter \
  --out feed-report.html
```

JSON вместо HTML:

```bash
jankhunter inspect logs/*.jhlog --json > inspect.json
```

По умолчанию, если в список попали несколько файлов вида `jh-session-log.YYYY-MM-DD.<index>.jhlog`, `inspect` определяет сессию и процесс по v9 identity внутри файла и берёт последнюю сессию для каждого процесса. Одна сессия хранится в одном файле: terminal-запись `SIZE_LIMIT` завершает сбор этой сессии и не создаёт следующий сегмент. Чтобы разобрать все выбранные сессии вместе:

```bash
jankhunter inspect logs/*.jhlog --all-sessions --out report.html
```

Если файл выгружен во время продолжающегося сбора после `JankHunter.flush()`, его статус
`open_clean` нормален: CLI читает снимок до последнего целого чанка, не снижает уровень доверия
и сообщает, что FINAL seal появится при завершении runtime. Служебные notices и реальные
предупреждения качества находятся в закрытом разделе в самом конце HTML и в конце text output.

## Данные Gradle-Плагина

Обычный `.jhlog` самодостаточен: имена реально выполненных ASM-методов и runtime-рёбра
раскрываются системным CLI без доступа к проекту и его build-директории. Gradle artifacts ниже
нужны только для расширенного developer-анализа: статического class graph, ASM-диагностики,
DI-каталога и mapping после R8/ProGuard.

Для раскрытия владельцев, классов и диагностики сборки достаточно передать каталог варианта:

```bash
jankhunter inspect logs/*.jhlog \
  --artifacts-dir ../android/sample-app/build/generated/jankhunter/debug \
  --out report.html
```

Автопоиск Gradle artifacts выключен в стандартном режиме. Он включается только явным
developer-ключом `--external-symbols`; CLI из `<project>/.jankhunter/bin` тогда ищет полный набор
`*/build/generated/jankhunter/<variant>/` внутри проекта и выбирает самый свежий. Для системного
CLI или нескольких вариантов передавайте `--artifacts-dir` явно.

Компактный внешний режим должен быть включён ещё при сборке приложения через
`JankHunterSymbolMode.STABLE_EXTERNAL`. Анализ такого лога запускается только явно:

```bash
jankhunter inspect logs/*.jhlog \
  --external-symbols \
  --artifacts-dir ../android/sample-app/build/generated/jankhunter/debug \
  --out report.html
```

Без `--external-symbols` CLI остановится с понятной ошибкой вместо отчёта с именами
`stable:...`. Без `--artifacts-dir`/`--owner-map` внешний режим также не запускается.

Отдельные файлы по-прежнему можно передать вручную; явные file-флаги имеют приоритет над
значениями из каталога:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map ../android/sample-app/build/generated/jankhunter/debug/owner-map.json \
  --owner-map ../feature-feed/build/generated/jankhunter/debug/owner-map.json \
  --mapping ../android/sample-app/build/outputs/mapping/debug/mapping.txt \
  --class-graph ../android/sample-app/build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics ../android/sample-app/build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --di-catalog ../android/sample-app/build/generated/jankhunter/debug/di-catalog.jsonl \
  --out report.html
```

Что дают флаги:

- `--artifacts-dir`: одним каталогом подключает owner-map, class graph, ASM diagnostics и доступный DI catalog.
- `--external-symbols`: разрешает developer-only логи, где имена методов вынесены в owner-map;
  требует matching `--artifacts-dir` или хотя бы один `--owner-map`.
- `--owner-map`: раскрывает сгенерированные владельцы в `class.method`. Флаг повторяется для
  каждого инструментированного Android-модуля, например отдельно для `app` и `feature-feed`.
  Все карты обязаны иметь один 16-байтный `symbolNamespace`, совпадающий с каждым входным
  `.jhlog`; CLI также отклоняет один stable ID, связанный с разными владельцами в разных картах.
  Обе проверки fail-closed: отчёт при несовместимом или неоднозначном наборе не создаётся.
  Namespace описывает stable-ID algorithm и owner-map schema, а не revision исходников, поэтому
  для полного раскрытия имён передавайте карты всех модулей именно от анализируемой версии.
- `--mapping`: раскрывает сокращённые имена после R8 или ProGuard.
- `--class-graph`: добавляет статические связи, горячие пути и узлы графа влияния.
- `--instrumentation-diagnostics`: добавляет отчёт о совпавших и пропущенных ASM-перехватчиках.
- `--di-catalog`: добавляет фиолетовую вкладку «DI-каталог» с build-time связями Dagger/Hilt/Koin.

В DI-отчёте действует жёсткая семантическая граница: «Build-time DI-связь. Это не ссылка
удержания, не runtime-вызов и не доказательство утечки. DI-данные не влияют на score, severity
или evidence». Каталог также не участвует в графе влияния, leak analysis, compare delta или gate.

## Утечки Памяти

Лёгкий режим не требует HPROF:

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

В этом случае вкладка «Утечки памяти» в `report.html` покажет удержанные объекты из `.jhlog`, вероятного держателя, экран, сценарий, шаг, возраст и рекомендации.

Runtime-сигналы разделены по силе доказательства:

- `time_only`: объект оставался жив после заданной задержки; GC не подтвержден, это только повод для проверки.
- `after_explicit_gc`: объект остался достижим после запрошенного явного GC; сигнал сильнее, но без цепочки ссылок все еще не доказывает утечку.
- `confirmed_hprof/path`: анализ HPROF нашел путь от распознанного корня GC до объекта. Это подтверждает удержание в момент дампа, а ожидаемость ссылки нужно проверить по жизненному циклу.

Потери runtime-событий, переполнение словаря и усечение HPROF явно снижают confidence соответствующих строк отчета.

Если рядом лежит `retained-*.hprof`, утилита подключит его сама. Для явного пути:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-dump dumps/checkout.hprof \
  --out report.html
```

Можно передать уже подготовленные доказательства в JSON:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-evidence heap-evidence.json \
  --out report.html
```

Минимальный пример:

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

С HPROF отчёт строит путь `GC root -> holder field -> retained object`, показывает размер удержания, альтернативные пути, чеклист расследования, примеры исправления и шаги проверки.

## Compare

Базовый и проверяемый прогоны:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --out compare.html
```

В отчёте есть:

- сводная панель базы и кандидата;
- матрица регрессий по сети, плавности, памяти, контексту и проблемным окнам;
- проверка когорт: устройство, сеть, версия приложения, SDK, процесс;
- таблицы «где изменилось»;
- сравнение утечек со статусами `new`, `worse`, `same`, `better`, `resolved`;
- математическое сравнение;
- граф влияния кандидата;
- подробности каждого лога.

С файлами Gradle-плагина:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --owner-map owner-map.json \
  --mapping mapping.txt \
  --class-graph class-graph.jsonl \
  --instrumentation-diagnostics instrumentation-diagnostics.jsonl \
  --out compare.html
```

JSON:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --json > compare.json
```

Дампы памяти можно передать отдельно:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --baseline-heap-dump old/heap.hprof \
  --candidate-heap-dump new/heap.hprof \
  --out compare.html
```

## Пороговая Проверка

Для проверки сборки можно задать пороги:

```json
{
  "max_severity": "medium",
  "min_confidence": "medium",
  "require_clean_cohorts": true,
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

Запуск:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --thresholds thresholds.json \
  --out compare.html
```

Если проверка падает, команда возвращает код `1`, но HTML всё равно сохраняется. Это удобно: сборка сказала «ку», а отчёт объяснил почему.

Поля `max_new: 0` и `max_worse: 0` сами по себе не включают строгий режим, потому что ноль является значением по умолчанию. Для строгой проверки используйте `fail_on_new` и `fail_on_worse`.

## Scorecard

`scorecard` нужен для оценки готовности данных и сравнения:

```bash
jankhunter scorecard \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --baseline-heap-dump old/heap.hprof \
  --candidate-heap-dump new/heap.hprof \
  --out scorecard.json
```

Смотрите поля `summary.go_no_go` и `summary.next_actions`. Там будет статус `go`, `qa_only` или `blocked`, а также список следующих действий: собрать больше логов на когорту, добавить HPROF, выровнять устройства или включить дополнительные доказательства.

## Problems

Реестр проблем кода:

```bash
jankhunter problems logs/*.jhlog --out problems.csv
jankhunter problems logs/*.jhlog --format json --out problems.json
```

Наборы данных:

```bash
jankhunter problems logs/*.jhlog --dataset code-problems --out problems.csv
jankhunter problems logs/*.jhlog --dataset leaks --out leaks.csv
jankhunter problems logs/*.jhlog --dataset influence --out influence.csv
jankhunter problems logs/*.jhlog --dataset math-findings --out math.csv
```

`code-problems` содержит строки вида `класс -> метод -> экран/сценарий/шаг/маршрут -> доказательства -> рекомендация`.

`leaks` добавляет `gc_root_category`, `chain_fingerprint`, `alternative_paths`, `investigation_steps`, `fix_examples` и `verification_steps`.

Этот режим полезен для задачи в системе отслеживания, обзора изменений и плагина для Android Studio.

## Export И Size

Сырые события:

```bash
jankhunter export logs/*.jhlog --out events.jsonl
```

Размер логов и вклад типов событий:

```bash
jankhunter size logs/*.jhlog
jankhunter size logs/*.jhlog --json
```

`size` показывает размер файла, распакованного тела, степень сжатия и вклад каждого типа событий. Если лог растёт как пепелац без тормозов, начинать стоит отсюда.

## Как Забрать Логи С Android

По умолчанию Android-библиотека пишет:

```text
context.filesDir/jankhunter/jh-session-log.YYYY-MM-DD.<index>.jhlog
```

Одна сессия сбора пишется в один append-only файл. При достижении лимита размера writer записывает terminal `SIZE_LIMIT`, запечатывает этот же файл и завершает сессию без файла-продолжения.

Через `adb`:

```bash
APP_ID=com.myapp
mkdir -p logs

adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs

jankhunter inspect logs/*.jhlog --out report.html
```

Готовый помощник для отладочного приложения и отчёта утечек:

```bash
cli/scripts/collect-android-leak-report.sh \
  --package com.myapp \
  --out /tmp/jankhunter-leaks \
  --cli ./cli/bin/jankhunter
```

Скрипт забирает `files/jankhunter`, находит `.jhlog` и `.hprof`, запускает `inspect` и создаёт один самодостаточный HTML.

## Математический Отчёт

Вкладка «Математический анализ» в `report.html` и `compare.html` добавляет более тяжёлые методы:

- временные интервалы и мини-графики;
- робастная статистика;
- точки изменения;
- автокорреляция и пики преобразования Фурье;
- сетевые циклы;
- интегральная нагрузка;
- Марковская модель состояний;
- граф причинности;
- справка по каждому методу.

Главная идея: сначала смотрим обычный отчёт, потом открываем `λ Анализ`, если нужно понять не только «что болит», но и «почему оно болит именно так».

## Граф Влияния

Вкладка «Граф влияния» в `report.html` и `compare.html` показывает классы, которые чаще всего совпали с симптомами: паузами главного потока, сетевыми хвостами, рывками интерфейса, ростом памяти, удержанными объектами и спамом логами.

Лучший режим:

```bash
jankhunter inspect logs/*.jhlog \
  --owner-map build/generated/jankhunter/debug/owner-map.json \
  --mapping app/build/outputs/mapping/debug/mapping.txt \
  --class-graph build/generated/jankhunter/debug/class-graph.jsonl \
  --instrumentation-diagnostics build/generated/jankhunter/debug/instrumentation-diagnostics.jsonl \
  --di-catalog build/generated/jankhunter/debug/di-catalog.jsonl \
  --out report.html
```

Если `--class-graph` не передан, отчёт всё равно покажет подозреваемые классы из `.jhlog`, но без статических связей между классами. Если после R8 или ProGuard не передан `--mapping`, имена могут выглядеть как `a.b.c`.

## ASM-Диагностика

Вкладка «ASM диагностика» показывает:

- классы, попавшие в сопоставитель;
- сработавшие перехватчики;
- неподдержанные сигнатуры;
- пропущенные методы;
- области аннотаций `@JankHunterFlow`, `@JankHunterScreen`, `@JankHunterTrace`, `@JankHunterOwner`;
- предупреждения о неполных или ошибочных строках диагностики.

Это первый раздел, куда стоит идти, если вы ожидали перехватчик, а в отчёте нет нужного сигнала.

## Формат `.jhlog`

Формат бинарный, chunked и рассчитан на безопасное чтение активного файла:

- время и thread ID фиксируются producer-потоком до постановки события в очередь;
- строки лежат в словаре;
- события length-delimited и ссылаются на локальные либо стабильные symbol IDs;
- по умолчанию stable ASM IDs имеют встроенные определения `stable ID -> class.method`,
  отделённые от лимитированного runtime-словаря;
- контекст `screen/owner/flow/step` атомарно входит в событие;
- повтор контекста внутри chunk кодируется флагом `same-context`;
- граф вызовов времени выполнения хранится агрегатами `caller_id -> callee_id`;
- каждый chunk имеет CRC, независимый gzip payload и commit trailer;
- незакоммиченный хвост активного файла отделяется от реального повреждения;
- loss/overflow/truncation публикуются накопительными quality snapshots;
- текущая схема: `FormatVersion=9`.

Семантика числовых метрик:

- `AVERAGE`: среднее значение, утилита объединяет `sum/count`.
- `LAST`: последние уровни и идентификаторы, где среднее бессмысленно.
- `STATE`: состояния вроде батареи, теплового режима или причины завершения процесса.
- `BOOLEAN_RATE`: доля `true` за окно.

Отрицательные пользовательские gauge-значения не пишутся как обычные числовые метрики. Библиотека считает их ошибкой ввода и увеличивает `jankhunter.metric.invalid_negative.gauge.count`.

## Проверки

```bash
make test
make build
./bin/jankhunter sample --out /tmp/sample.jhlog
./bin/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
./bin/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
npm run visual-regression
```

Очистка сборочных файлов:

```bash
make clean
```
