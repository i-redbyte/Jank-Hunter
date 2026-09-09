# Командная утилита Jank Hunter

`jankhunter` - локальная утилита для файлов `.jhlog`. Она создаёт отчёты по одному прогону, сравнивает два набора данных, анализирует удержание памяти, выгружает реестр проблем и проверяет пороги регрессии. Сервер не нужен; результаты сохраняются в HTML, JSON, CSV или JSONL.

Текущая версия утилиты - `1.0.0`, поддерживаемый формат JHLOG - `5.0.0`. Это не версия Android SDK: для Android используется `1.0.9`.

## Сборка и установка

```bash
make build
./bin/jankhunter version
```

Ожидаемый вывод:

```text
Jank Hunter CLI 1.0.0
.jhlog format 5.0.0
```

Если Go не найден, Makefile загрузит Go `1.22.12` в `cli/.tools/go`. Системные каталоги и системная версия Go не изменяются.

Установка:

```bash
make install
make install PREFIX="$HOME/.local"
```

Сборка для другой платформы:

```bash
make build BUILD_OS=linux BUILD_ARCH=amd64 OUT=bin/jankhunter-linux-amd64
make build BUILD_OS=darwin BUILD_ARCH=arm64 OUT=bin/jankhunter-darwin-arm64
```

Выпуск архивов для платформ из `PLATFORMS`:

```bash
make release VERSION=1.0.0
```

## Быстрая проверка

```bash
./bin/jankhunter sample --out /tmp/baseline.jhlog
./bin/jankhunter sample --out /tmp/candidate.jhlog
./bin/jankhunter inspect /tmp/baseline.jhlog --out /tmp/report.html
./bin/jankhunter compare \
  --baseline /tmp/baseline.jhlog \
  --candidate /tmp/candidate.jhlog \
  --out /tmp/compare.html
./bin/jankhunter size /tmp/baseline.jhlog
```

HTML самодостаточен: связанные страницы отчёта встраиваются в итоговый файл, внешние ресурсы не требуются. Флаг `--report-style` удалён и не поддерживается.

Для показа на большом экране:

```bash
jankhunter inspect logs/*.jhlog --presentation --out report.html
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --presentation \
  --out compare.html
```

Декоративное движение фона включается только явно:

```bash
jankhunter inspect logs/*.jhlog --animated-background --out report.html
```

## Команды

| Команда | Назначение |
| --- | --- |
| `sample` | Создать демонстрационный `.jhlog`. |
| `inspect` | Проанализировать один или несколько журналов. |
| `compare` | Сравнить базовый и проверяемый наборы. |
| `problems` | Выгрузить проблемы в CSV или JSON. |
| `scorecard` | Оценить пригодность данных для решения о выпуске. |
| `export` | Выгрузить декодированные события в JSONL. |
| `size` | Показать состав и степень сжатия журналов. |
| `version` | Вывести версии утилиты и JHLOG. |
| `help` | Вывести справку. |

```bash
jankhunter help
```

Неизвестные параметры отклоняются. Входные пути приводятся к каноническому виду, дубликаты одного файла исключаются, а пересечение базового и проверяемого наборов считается ошибкой.

## Анализ одного набора

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Отчёт включает:

- итог и полноту диагностических данных;
- ранжированный список проблем с местоположением, доказательствами и рекомендациями;
- HTTP, WebSocket, базу данных, компоненты Android и Binder IPC;
- кадры интерфейса, паузы главного потока и операции пользователя;
- память, удержанные объекты и необязательный HPROF;
- источники работы, экраны, владельцев и классы;
- граф влияния кода и математический анализ;
- качество записи, целостность сегментов и причины потерь.

Фильтры:

```bash
jankhunter inspect logs/*.jhlog \
  --route /feed \
  --screen Feed \
  --owner FeedRepository \
  --class CheckoutPresenter \
  --out feed-report.html
```

JSON в стандартный вывод:

```bash
jankhunter inspect logs/*.jhlog --json > inspect.json
```

Для канонических имён `jh-session-log.YYYY-MM-DD.<run-id>.<index>.jhlog` утилита проверяет цепочку сегментов, область процессов и ожидаемый состав процессов. По умолчанию выбирается последняя целая когорта одного запуска. Чтобы намеренно объединить все выбранные запуски:

```bash
jankhunter inspect logs/*.jhlog --all-sessions --out report.html
```

Статус `open_clean` допустим для снимка активной записи: анализируется последний полностью подтверждённый блок. Повреждение, незакрытый хвост и реальные потери показываются отдельно.

## Артефакты Android-сборки

Журнал сам содержит имена фактически выполненных инструментированных методов. Дополнительные файлы нужны для статического графа, диагностики внедрения, DI, компонентов Android и раскрытия имён после R8/ProGuard.

Каталог одного варианта:

```bash
jankhunter inspect logs/*.jhlog \
  --artifacts-dir app/build/generated/jankhunter/debug \
  --mapping app/build/outputs/mapping/debug/mapping.txt \
  --out report.html
```

`--artifacts-dir` требует непустые `artifact-metadata.json`, `class-graph.jsonl` и `instrumentation-diagnostics.jsonl`, проверяет пространство символов по заголовкам `.jhlog` и при наличии подключает `di-catalog.jsonl` и `android-components-catalog.jsonl`.

Отдельные файлы можно передать явно:

```bash
jankhunter inspect logs/*.jhlog \
  --class-graph class-graph.jsonl \
  --instrumentation-diagnostics instrumentation-diagnostics.jsonl \
  --di-catalog di-catalog.jsonl \
  --android-components-catalog android-components-catalog.jsonl \
  --database-evidence database-evidence.json \
  --mapping mapping.txt \
  --out report.html
```

Явные параметры имеют приоритет над путями из `--artifacts-dir`. `database-evidence.json` является отдельным входом расширенного анализа базы данных и не создаётся стандартной задачей Gradle-плагина.

DI-каталог описывает статические связи Dagger, Hilt и поддерживаемых определений Koin. Он не считается графом вызовов, ссылкой удержания или доказательством утечки и не влияет на важность проблемы.

## Удержание памяти и HPROF

Без HPROF отчёт показывает события удержания из `.jhlog`:

```bash
jankhunter inspect logs/*.jhlog --out report.html
```

Сила доказательства различается:

- `time_only` - объект оставался жив после задержки; сборка мусора не подтверждена;
- `after_explicit_gc` - объект пережил запрошенную сборку мусора, но путь ссылок неизвестен;
- `confirmed_hprof/path` - HPROF содержит путь от распознанного корня сборщика мусора до объекта.

Файл `retained-*.hprof` рядом с журналами обнаруживается автоматически. Явный путь:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-dump dumps/checkout.hprof \
  --out report.html
```

Уже подготовленные доказательства:

```bash
jankhunter inspect logs/*.jhlog \
  --heap-evidence heap-evidence.json \
  --out report.html
```

Наличие пути в HPROF подтверждает удержание в момент дампа, но ожидаемость ссылки всё равно нужно оценивать по жизненному циклу объекта.

## Сравнение прогонов

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --out compare.html
```

Сравнение включает изменения метрик, новые и исправленные проблемы, удержания памяти, состав устройств, процессов, версий приложения, SDK и сетей. Частотные показатели нормализуются по длительности, а несопоставимые данные помечаются как не сравниваемые.

Машиночитаемый вывод:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --json > compare.json

jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --csv > compare.csv
```

Одновременно использовать `--json` и `--csv` нельзя.

Отдельные дампы памяти:

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --baseline-heap-dump old/retained.hprof \
  --candidate-heap-dump new/retained.hprof \
  --out compare.html
```

## Проверка порогов

`compare` может применить JSON-контракт:

```json
{
  "max_severity": "medium",
  "min_confidence": "medium",
  "require_clean_cohorts": true,
  "metrics": {
    "HTTP p95": {"max_regression_pct": 12},
    "UI jank rate": {"max_regression_abs": 1.5}
  },
  "problems": {
    "max_high": 0,
    "fail_on_new": true,
    "fail_on_regressed": true
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

```bash
jankhunter compare \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --thresholds thresholds.json \
  --out compare.html
```

При нарушении порога команда возвращает код `1`, но успевает сохранить HTML. Ошибки вызова без команды возвращают код `2`; прочие ошибки анализа возвращают код `1`.

## Оценка пригодности данных

```bash
jankhunter scorecard \
  --baseline "old/*.jhlog" \
  --candidate "new/*.jhlog" \
  --out scorecard.json
```

В `summary.go_no_go` возвращается `go`, `qa_only` или `blocked`, а `summary.next_actions` перечисляет необходимые действия: выровнять когорты, собрать больше запусков, добавить HPROF или устранить потери.

## Выгрузка проблем

```bash
jankhunter problems logs/*.jhlog --out problems.csv
jankhunter problems logs/*.jhlog --format json --out problems.json
```

Наборы данных:

```bash
jankhunter problems logs/*.jhlog --dataset problems --out problems.csv
jankhunter problems logs/*.jhlog --dataset code-problems --out code.csv
jankhunter problems logs/*.jhlog --dataset leaks --out leaks.csv
jankhunter problems logs/*.jhlog --dataset influence --out influence.csv
jankhunter problems logs/*.jhlog --dataset math-findings --out math.csv
```

`problems` - канонический реестр с отпечатком, детектором, категорией, важностью, достоверностью, местоположением, доказательствами и рекомендациями. Остальные наборы предоставляют специализированные представления.

## Выгрузка событий и состав журнала

```bash
jankhunter export logs/*.jhlog --out events.jsonl
jankhunter size logs/*.jhlog
jankhunter size logs/*.jhlog --json
```

`export` поддерживает формат `jsonl`. `size` показывает физический и распакованный размер, число событий, словарных и служебных записей, количество подтверждённых блоков, степень сжатия и вклад типов событий.

## Копирование журналов с Android

Стандартный путь:

```text
context.filesDir/jankhunter/jh-session-log.YYYY-MM-DD.<run-id>.<index>.jhlog
```

```bash
APP_ID=com.example.app
mkdir -p logs
adb shell run-as "$APP_ID" ls files/jankhunter
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs
jankhunter inspect logs/*.jhlog --out report.html
```

Готовый сценарий для отладочного приложения:

```bash
./scripts/collect-android-leak-report.sh \
  --package com.example.app \
  --out /tmp/jankhunter-leaks \
  --cli ./bin/jankhunter
```

## Математический анализ и граф влияния

Математическая страница включает проверку качества данных, робастную статистику, точки изменения, периодические сигналы, сетевые циклы, интегральную нагрузку, Марковскую модель и граф статистических связей. Эти результаты являются направлением расследования, а не автоматическим доказательством причины.

Граф влияния различает:

- фактически записанные связи `caller → callee` из `CALL_GRAPH`;
- статические связи из `class-graph.jsonl`;
- смешанные связи и узлы, сопоставленные с проблемами.

Если `CALL_GRAPH` выключен, фактическая цепочка вызовов отсутствует. Если не передан `class-graph.jsonl`, статические связи недоступны. Эти два источника не заменяют друг друга.

## Диагностика внедрения

При наличии `instrumentation-diagnostics.jsonl` отчёт показывает обработанные классы, совпавшие перехватчики, решения фильтра, пропущенные методы и неподдержанные сигнатуры. Это основной источник проверки, если ожидаемый сигнал отсутствует.

Актуальные аннотации Android: `@JankHunterOperation`, `@JankHunterScreen`, `@JankHunterOwner`, `@JankHunterIgnore`.

## Формат JHLOG 5.0.0

Утилита читает только JHLOG `5.0.0` и завершает анализ ошибкой для прежней версии. Формат включает:

- заголовок с идентификаторами запуска, процесса и сегмента;
- словарь с префиксным и токенизированным кодированием строк;
- компактные записи и колонночные микространицы высокочастотных событий;
- встроенные определения стабильных идентификаторов методов;
- агрегированные связи графа вызовов;
- точные снимки качества и причин отклонения событий;
- контрольную сумму заголовка и данных каждого блока;
- маркер подтверждения блока и отдельный финальный блок;
- цепочку SHA-256 между последовательными сегментами;
- выбранную политикой формата компрессию блоков или секций.

Неподтверждённый хвост активного файла не интерпретируется как событие. Данные после финального блока, неверная цепочка, несовместимые возможности формата или несогласованные счётчики считаются повреждением.

## Проверки

```bash
make test
make build
./bin/jankhunter version
./bin/jankhunter sample --out /tmp/sample.jhlog
./bin/jankhunter inspect /tmp/sample.jhlog --out /tmp/report.html
./bin/jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
npm run visual-regression
```

Очистка сборочных файлов:

```bash
make clean
```
