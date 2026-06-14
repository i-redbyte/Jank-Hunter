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
make release VERSION=0.1.0
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
/tmp/report-influence.html
/tmp/compare.html
/tmp/compare-math.html
/tmp/compare-influence.html
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
  --out report.html
```

`--owner-map` помогает красиво раскрыть владельцев работ, а `--class-graph` добавляет статические связи между классами. С ним рядом с `report.html` появится `report-influence.html`.

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

Если gate падает, команда возвращает exit code `1`. HTML при этом сохраняется, чтобы было что открыть и посмотреть.

## export

Экспорт событий в JSONL:

```bash
jankhunter export /tmp/sample.jhlog --out /tmp/sample.jsonl
```

Это удобно, если нужно быстро проверить сырые события или скормить их другому инструменту.

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

Если лог получили через FileProvider/share sheet, просто положите файл в папку и передайте его CLI:

```bash
jankhunter inspect ~/Downloads/*.jhlog --out ~/Downloads/jankhunter-report.html
```

## Математический отчет

Для `inspect --out report.html` рядом создается:

```text
report-math.html
report-influence.html
```

Для `compare --out compare.html`:

```text
compare-math.html
compare-influence.html
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
- числовые строки и даты могут BCD-паковаться, если так меньше.
- Android runtime по умолчанию сжимает тело файла gzip-потоком после magic-заголовка, CLI читает и сжатый, и старый несжатый body.

До фиксации первой стабильной версии формат можно ломать и улучшать. Сейчас CLI читает текущую схему `FormatVersion=4`.

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
