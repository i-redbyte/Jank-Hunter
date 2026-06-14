# Jank Hunter

Jank Hunter помогает поймать то, что обычно сложно доказать словами: дерганый UI, длинные паузы главного потока, медленную сеть, рост памяти, удержанные объекты, слишком шумные логи и странные различия между двумя сборками.

Идея простая: Android-приложение пишет компактный `.jhlog`, а CLI на машине разработчика превращает его в нормальный HTML-отчет. Без сервера, без базы, без загрузки данных наружу.

## Что внутри

- `android/` - runtime SDK, OkHttp-интеграция, Gradle plugin с ASM-инструментацией и sample app.
- `cli/` - утилита `jankhunter`, которая читает `.jhlog`, строит inspect/compare отчеты, экспортирует JSONL и умеет работать в CI.

Отчеты автономные: обычный HTML с CSS внутри. Их можно открыть локально, приложить к задаче, положить в CI artifacts или отправить команде.

## Быстрый старт CLI

```bash
cd cli
make build
./bin/jankhunter sample --out /tmp/sample.jhlog
./bin/jankhunter inspect /tmp/sample.jhlog --out /tmp/jankhunter-report.html
./bin/jankhunter compare --baseline /tmp/sample.jhlog --candidate /tmp/sample.jhlog --out /tmp/jankhunter-compare.html
```

`make build` сам скачает Go в `cli/.tools/go`, если Go не найден в системе. После сборки бинарник лежит в `cli/bin/jankhunter`.

Установка в систему:

```bash
cd cli
make install
```

Если не хочется ставить в `/usr/local/bin`:

```bash
make install PREFIX="$HOME/.local"
```

## Быстрый старт Android

Для локальной проверки можно собрать и запустить sample app:

```bash
./run-sample-app.sh
```

Скрипт поднимет или использует уже запущенный emulator/device, установит sample app и даст интерактивные команды:

```text
log
report
stop
open
help
quit
```

`log` и `stop` вытаскивают `.jhlog` из приложения и генерируют HTML-отчет в `tmp/sample-app-.../pull-.../report.html`.

Для своего приложения базовое подключение обычно выглядит так:

```kotlin
dependencies {
    debugImplementation("io.jankhunter:jankhunter-runtime:0.1.0-SNAPSHOT")
    debugImplementation("io.jankhunter:jankhunter-okhttp3:0.1.0-SNAPSHOT")
}
```

Gradle plugin подключайте только на debug/QA сборки и сначала ограничивайте include-пакеты. Если проект огромный и перечислять модули больно, есть `includeWholeApplication = true` плюс `excludePackages(...)`.

Подробности по Android лежат в [android/README.md](android/README.md), по CLI - в [cli/README.md](cli/README.md).

Автоподключение в существующий Android-проект на macOS:

```bash
scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --include-package com.myapp.feature \
  --include-package com.myapp.data \
  --exclude-packages com.myapp.generated,com.myapp.di \
  --runtime-call-graph
```

Скрипт публикует Android SDK в `~/work/MyApp/.jankhunter/maven`, добавляет этот Maven repo в `settings.gradle(.kts)`, подключает Gradle plugin/dependencies в указанный модуль и создает `jankHunter { ... }` конфиг. Перед правками он оставляет backup в `.jankhunter-backups/`.

## Что собирается

- HTTP: длительность запроса, DNS/connect/TTFB, ошибки, байты, route, owner.
- UI: FPS, доля медленных кадров, p95/p99 кадра, экраны.
- Главный поток: длинные паузы и источники работ.
- Память: PSS, Java/native heap, свободная RAM, retained objects.
- Контекст устройства: Android/API/security patch, ABI, сеть/VPN, батарея, storage, рут-доступ.
- Пользовательские counters/gauges.
- Owner attribution: ручной `JankHunter.withOwner(...)` и ASM-generated owners.
- Граф влияния кода: классы, флоу, проблемные окна, лог-спам, runtime-вызовы и build-time ASM-связи между классами.

CLI строит два основных режима:

- `inspect` - один лог или набор логов, чтобы понять текущий прогон.
- `compare` - baseline против candidate, чтобы увидеть регрессии, когорты и конкретные места, где стало хуже.

Рядом с основным HTML создается математический отчет: `report-math.html` или `compare-math.html`. Он открывается из зеленой кнопки `λ Анализ`. Если есть owner/flow-сигналы, CLI также создает `report-influence.html` или `compare-influence.html` с графом влияния кода.

## Проверки

CLI:

```bash
cd cli
make test
```

Android:

```bash
cd android
./gradlew detekt :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon
```

End-to-end через emulator/device:

```bash
./scripts/android-e2e.sh
```

Он собирает sample app, запускает instrumentation test, вытаскивает `.jhlog` и кладет отчет в:

```text
reports/android-e2e/report.html
```

## Важные принципы

- Не грузить приложение тяжелой диагностикой на каждом событии.
- Писать high-frequency данные агрегатами.
- Держать runtime без лишних зависимостей.
- Все спорное включать явно: ASM, корутины, JankStats, release-сборки.
- Сначала компактный машинный лог, потом удобный человеческий отчет на стороне CLI.
