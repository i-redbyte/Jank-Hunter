# Jank Hunter

Jank Hunter помогает поймать то, что обычно ускользает в стиле «у меня всё плавно»: рывки интерфейса, длинные паузы главного потока, медленную сеть, рост памяти, удержанные объекты, шумные логи и регрессии между двумя прогонами.

Схема простая. Android-приложение пишет компактный `.jhlog`, а локальная утилита `jankhunter` превращает его в набор автономных HTML-отчётов. Сервер не нужен, база не нужна, данные никуда не отправляются. Всё остаётся на машине разработчика или в файлах проверки сборки. Почти как гравицапа, только без необходимости искать чатланина с визой.

## Что В Репозитории

- `android/`: runtime, аннотации, опциональная интеграция с OkHttp, Gradle-плагин с ASM-внедрением и пример приложения.
- `cli/`: утилита `jankhunter`, которая читает `.jhlog`, строит отчёты, выгружает таблицы проблем и выдаёт оценку готовности для проверок.
- `plugin-as/`: плагин для Android Studio и IntelliJ IDEA. Он запускает `jankhunter`, подтягивает логи с устройства, открывает отчёты и показывает таблицу проблем с переходом в исходники.
- `scripts/`: помощники для подключения Jank Hunter к существующему Android-проекту, проверки Gradle-плагина и сквозного прогона на устройстве.
- `assets/readme/`: свежие снимки HTML-отчётов, снятые из текущего кода.

## Как Выглядят Отчёты

Снимки ниже собраны командой `npm run visual-regression` из каталога `cli/`. Это не макеты, а реальные вкладки самодостаточного HTML, который создаёт утилита.

### Один Прогон

Верх отчёта даёт контекст устройства, число событий, длительность прогона и быстрые переходы к дополнительным страницам.

![Верх inspect-отчёта](assets/readme/inspect-hero.webp)

Матрица сигналов показывает задержки сети, рывки интерфейса, частоту кадров, паузы главного потока, память и краткий срез графа влияния.

![Матрица сигналов](assets/readme/inspect-signals.webp)

Раздел сценариев связывает экран, пользовательский путь, шаг, источник работы и симптомы. Это место, где фраза «где-то тормозит» наконец получает адрес прописки.

![Сценарии и причины](assets/readme/inspect-flows.webp)

### Утечки Памяти

Вкладка «Утечки памяти» в `report.html` работает в лёгком режиме даже без дампа памяти. Если рядом есть `retained-*.hprof` или передан `--heap-dump`, отчёт добавляет путь от корня сборщика мусора до удержанного объекта.

![Проводник утечек](assets/readme/leaks-explorer.webp)

### Математический Разбор

Математическая страница не заменяет обычный отчёт, а помогает понять форму проблемы: качество данных, устойчивость распределений, точки изменения, повторяемость, интегральную нагрузку и причинные связи.

![Сводка математического анализа](assets/readme/math-summary.webp)

Сетевые циклы ищут повторяющиеся маршруты и всплески. Если циклов нет, отчёт честно пишет «готово», без шаманского танца вокруг нулей.

![Сетевые циклы](assets/readme/math-network-loops.webp)

Интегральная нагрузка считает не только пик, но и накопленную площадь боли во времени: рывки интерфейса, сетевые хвосты, память и восстановление.

![Интегральная нагрузка](assets/readme/math-integral.webp)

Граф причинности связывает симптомы, маршруты, фазы сети, источники работ и экраны. Он помогает идти от признака к месту в коде, а не гадать по кофейной гуще.

![Граф причинности](assets/readme/math-causal-graph.webp)

### Код, Внедрение И Сравнение

Граф влияния показывает классы, которые чаще всего совпали с проблемами. Узлы и связи нужны для ранжирования расследования, а не для автоматического приговора.

![Граф влияния кода](assets/readme/influence-graph.webp)

ASM-диагностика показывает, какие перехватчики реально совпали с байткодом, какие решения сопоставителя сработали и какие сигнатуры оказались неподдержанными.

![ASM-диагностика](assets/readme/diagnostics-overview.webp)

`compare` сравнивает базовый и проверяемый прогоны: задержки, плавность, память, трафик, удержанные объекты, когорты и проблемные классы.

![Сравнение прогонов](assets/readme/compare-overview.webp)

## Быстрый Старт

Соберите утилиту и создайте демонстрационный лог:

```bash
cd cli
make build
./bin/jankhunter sample --out /tmp/baseline.jhlog
./bin/jankhunter sample --out /tmp/candidate.jhlog
./bin/jankhunter inspect /tmp/baseline.jhlog --out /tmp/jankhunter-report.html
./bin/jankhunter compare --baseline /tmp/baseline.jhlog --candidate /tmp/candidate.jhlog --out /tmp/jankhunter-compare.html
```

`make build` использует установленный Go. Если Go не найден, Makefile скачает Go `1.22.12` в `cli/.tools/go` и не тронет системные каталоги. Текущая версия утилиты: `1.0.1`, формат `.jhlog`: `9`.

Установка команды:

```bash
cd cli
make install
make install PREFIX="$HOME/.local"
```

## Пример Android-Приложения

Для живой проверки можно запустить пример приложения:

```bash
./run-sample-app.sh
```

Скрипт найдёт устройство или запустит эмулятор, установит пример и даст команды:

```text
log
report
stop
open
help
quit
```

Команды `log`, `report` и `stop` забирают `.jhlog` через `adb run-as` и создают HTML-отчёт в `tmp/sample-app-.../pull-.../report.html`.

Пример приложения сейчас работает как маленький полигон: чистый базовый прогон, шумный кандидат, переключатель сбора, лаборатория производительности, лаборатория утечек, сравнение с LeakCanary и сценарии для `compare`.

## Подключение К Своему Приложению

Минимальное подключение обычно выглядит так:

```kotlin
import io.jankhunter.gradle.JankHunterFeatureMode

plugins {
    id("io.jankhunter.android") version "1.0.0"
}

dependencies {
    implementation("io.jankhunter:jankhunter-android-sdk:1.0.0")
}
```

`jankhunter-android-sdk` — единственная пользовательская dependency; runtime, annotations и
OkHttp/WebSocket support приходят транзитивно. ASM обрабатывает только классы текущего модуля
внутри его Android `namespace`; ручные include-пакеты могут безопасно расширить эту границу внутри модуля:

```kotlin
jankHunter {
    enabledBuildTypes.add("debug")
    verboseLogs = false
    sessionLogSizeLimitEnabled = true
    maxSessionLogSizeMiB = 16
    // Отдельный build-time каталог Dagger/Hilt/Koin; по умолчанию DISABLED.
    dependencyInjectionAnalysis = JankHunterFeatureMode.DISABLED

    instrument {
        includePackages("com.myapp.feature", "com.myapp.data")
        excludePackages("com.myapp.generated", "com.myapp.di")
        handlers = true
        executors = true
        // Opt-in: добавляет wrapper-объекты на coroutine builders.
        coroutines = false
        flowInteractions = true
        logSpam = true
        classGraph = true
        // Глубокий runtime-граф — только для короткой целевой диагностики.
        runtimeCallGraph = false
    }
}
```

MainLooper `Printer` и coroutine ASM по умолчанию выключены. Для UI-кадров JankStats имеет
приоритет, а Choreographer работает только как fallback, поэтому один кадр не попадает в отчёт
дважды.

Каждая сессия сбора создаёт один файл `jh-session-log.YYYY-MM-DD.<index>.jhlog`.
Внутренний ограничитель по умолчанию включён и запечатывает файл при 16 МиБ; его можно
настроить через `sessionLogSizeLimitEnabled` и `maxSessionLogSizeMiB`. Лимит пользовательского
`JankHunterBinaryStorage` действует всегда, а встроенное хранилище держит до 64 МиБ закрытых
сессий. Process/session identity находится внутри заголовка `.jhlog v9`, а не в имени файла.

OkHttp/WebSocket hooks используют support из `jankhunter-android-sdk`; дополнительных зависимостей для них нет.

Без Gradle-плагина можно вручную подключить `jankhunter-runtime` и вызвать `JankHunter.init(...)`, но ASM-внедрения и автоматически сгенерированного `JankHunterAutoInitProvider` в таком режиме не будет.

Если нужно быстро подключить существующий проект на macOS:

```bash
scripts/integrate-android-project.sh ~/work/MyApp
```

С ограничением ASM и включённым графом вызовов времени выполнения:

```bash
scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --include-package com.myapp.feature \
  --include-package com.myapp.data \
  --exclude-packages com.myapp.generated,com.myapp.di \
  --runtime-call-graph
```

Скрипт публикует Android-модули Jank Hunter в `.jankhunter/maven`, собирает `jankhunter` в `.jankhunter/bin`, добавляет репозиторий в настройки Gradle, прописывает `sdk.dir`, подключает зависимости и создаёт `jankHunter { ... }`. Перед правками он кладёт копии изменяемых файлов в `.jankhunter-backups/`.

## Что Собирается

- HTTP: длительность, DNS, соединение, время до первого байта, ошибки, байты, маршрут и владелец работы.
- WebSocket: события через обёртку слушателя.
- Интерфейс: окна кадров, частота кадров, доля медленных кадров, экраны.
- Главный поток: длинные паузы, источники работ и подозрительные окна.
- Память: PSS, Java heap, native heap, свободная память, удержанные объекты и, при явном разрешении, HPROF.
- Устройство: Android, API, патч безопасности, ABI, сеть, VPN, батарея, хранилище, признак root-доступа.
- Пользовательские счётчики и числовые метрики.
- Атрибуция: `JankHunter.withOwner(...)`, `@JankHunterOwner`, `@JankHunterIgnore`, `@JankHunterScreen`, `@JankHunterFlow`, `@JankHunterTrace`.
- Граф влияния: классы, сценарии, проблемные окна, спам логами, связи времени выполнения и статический граф ASM.
- Диагностика внедрения: совпавшие перехватчики, пропуски, неподдержанные сигнатуры и области аннотаций.
- DI-каталог по явному opt-in: build-time связи Dagger/Hilt/Koin без runtime tracing generated-кода.

## Что Создаёт Утилита

Для одного прогона создаётся один самодостаточный файл:

```text
report.html
```

Для сравнения — также один файл:

```text
compare.html
```

Обзор, математический анализ, утечки и граф влияния встроены во вкладки этого HTML. Флаг
`--instrumentation-diagnostics` добавляет вкладку «ASM диагностика», а `--di-catalog` — вкладку
«DI-каталог». DI-связи не являются ссылками удержания или runtime-вызовами и не влияют на score,
severity, evidence, граф влияния или анализ утечек. Файл можно передавать отдельно: внешние ресурсы
и соседние HTML ему не нужны.

Отдельные команды:

- `inspect`: один лог или группа логов.
- `compare`: базовый прогон против проверяемого.
- `problems`: CSV или JSON с проблемными местами.
- `scorecard`: JSON-оценка готовности данных и сравнения.
- `export`: сырые события в JSONL.
- `size`: профиль размера `.jhlog`.
- `version`: версия утилиты и формат лога.

## Проверки

Командная утилита:

```bash
cd cli
make test
npm run visual-regression
```

Android:

```bash
cd android
./gradlew detekt :jankhunter-gradle-plugin:test :jankhunter-okhttp3:testDebugUnitTest :jankhunter-runtime:testDebugUnitTest :sample-app:assembleDebug --no-daemon
```

Проверка Gradle-плагина как внешнего потребителя:

```bash
scripts/gradle-plugin-smoke.sh
```

Сквозной прогон на устройстве или эмуляторе:

```bash
./scripts/android-e2e.sh
```

Он собирает пример приложения, запускает проверку на устройстве, забирает `.jhlog` и кладёт отчёт в `reports/android-e2e/report.html`.

## Релизы

GitHub Actions собирает релиз по тегу `v*` или вручную из действия `Release`:

```bash
git tag v1.0.1
git push origin v1.0.1
```

В выпуск попадают:

- архив локального Maven-репозитория с отдельными артефактами runtime, annotations, опциональной OkHttp-интеграции, Gradle-плагина и маркера плагина.
- `jankhunter_<version>_darwin_amd64.tar.gz`: утилита для macOS Intel.
- `jankhunter_<version>_darwin_arm64.tar.gz`: утилита для macOS Apple Silicon.
- `checksums.txt`: суммы SHA-256.

## Принципы

- Высокочастотные данные пишутся агрегатами, а не потоком мелких событий.
- Всё тяжёлое включается явно: ASM, дампы памяти, расширенные перехватчики и релизные сборки.
- Отладочный прогон должен быть полезным без сервера и без особой церемонии.
- Отчёт должен вести от симптома к месту в коде. Если он просто говорит «всё плохо», это не отчёт, а пацак без гравицапы.
