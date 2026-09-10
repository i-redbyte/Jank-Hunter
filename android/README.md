# Jank Hunter для Android

Android-часть Jank Hunter автоматически внедряет диагностические перехватчики на этапе сборки, собирает события во время работы приложения и сохраняет их в `.jhlog`. Актуальная версия SDK и Gradle-плагина - `1.0.9`.

```properties
jankHunterVersion=1.0.9
```

## Модули

| Артефакт | Назначение |
| --- | --- |
| `jankhunter-android-sdk` | Единая пользовательская зависимость: среда выполнения, аннотации и поддержка OkHttp/WebSocket. |
| `jankhunter-gradle-plugin` | Настройка вариантов сборки, ASM-внедрение и дополнительные диагностические артефакты. |
| `jankhunter-runtime` | Среда выполнения и ручной API; отдельно обычно не подключается. |
| `jankhunter-annotations` | Аннотации владельцев, экранов, операций и исключений из внедрения. |
| `jankhunter-okhttp3` | Поддержка OkHttp и WebSocket; входит в `jankhunter-android-sdk`. |
| `jankhunter-workmanager` | Необязательная интеграция с WorkManager; подключается отдельно. |
| `sample-app` | Демонстрационное приложение и сквозной сценарий проверки. |

## Быстрое подключение

В модуле приложения добавьте плагин и единый SDK-артефакт одной версии:

```kotlin
plugins {
    id("io.jankhunter.android") version "1.0.9"
}

dependencies {
    implementation("io.jankhunter:jankhunter-android-sdk:1.0.9")
}
```

Минимальная настройка:

```kotlin
jankHunter {
    enabled.set(true)
    enabledBuildTypes.set(setOf("debug"))
    profile.set(io.jankhunter.gradle.JankHunterProfile.BALANCED)
    packages("com.example.app")
    storageLimitMiB(16)
}
```

По умолчанию Jank Hunter включён только для `debug`, использует профиль `BALANCED`, область `NAMESPACE_AND_PACKAGES`, главный процесс, автоматическую инициализацию и ограниченное файловое хранилище.

Во время сборки включённого варианта выводится строка:

```text
================JANK HUNTER 1.0.9 ENABLED================
```

## Инициализация и управление сбором

Gradle-плагин добавляет `JankHunterAutoInitProvider`, который запускает среду выполнения до `Application.onCreate()`. Дополнительный код приложения не требуется.

Для ручной инициализации:

```kotlin
jankHunter {
    autoInit.set(false)
}
```

```kotlin
class App : Application() {
    override fun onCreate() {
        super.onCreate()
        io.jankhunter.runtime.JankHunter.init(this)
    }
}
```

Публичный фасад жизненного цикла и хранилища:

```kotlin
JankHunter.isStarted()
JankHunter.isRuntimeEnabled()
JankHunter.setRuntimeEnabled(false, "проверка")
JankHunter.setRuntimeEnabled(true, "продолжение")
JankHunter.flush()
JankHunter.initDiagnostics()
JankHunter.shutdown()
```

`setRuntimeEnabled` временно останавливает или возобновляет приём событий без пересборки приложения. `flush()` отправляет уже принятые события на запись, но не запечатывает весь запуск.

Согласованный снимок файлов активных процессов:

```kotlin
val snapshot = JankHunter.captureLogSnapshot()
val archive = JankHunter.captureLogArchive(File(cacheDir, "jankhunter.zip"))
```

`captureLogSnapshot()` закрывает текущие сегменты и сразу продолжает сбор в новых. `captureLogArchive(...)` дополнительно создаёт один ZIP без повторного сжатия уже сжатых блоков JHLOG.

## Профили

Профиль задаёт исходный набор возможностей, режим сбора, число процессов и вместимость очереди. Явные настройки применяются поверх него.

| Профиль | Сбор | Очередь | Процессы | Возможности |
| --- | --- | ---: | --- | --- |
| `MINIMAL` | `BALANCED` | `8192` | `MAIN_ONLY` | `JANK_STATS` |
| `BALANCED` | `BALANCED` | `32768` | `MAIN_ONLY` | кадры, SQLite/Room, HTTP, асинхронная работа, операции, жизненный цикл, журналирование, граф классов, Compose, WorkManager и компоненты Android |
| `FULL` | `EXACT` | `65536` | `ALL` | все безопасные возможности, включая WebSocket, ввод-вывод, DI, граф вызовов и Binder IPC |
| `RELEASE_SAFE` | `BALANCED` | `16384` | `MAIN_ONLY` | ограниченный набор для релизной диагностики |
| `TARGETED` | `EXACT` | `65536` | `MAIN_ONLY` | только явно включённые возможности |

`FULL` намеренно не включает `MAIN_LOOPER`, `METHOD_COUNTERS` и `HEAP_DUMPS`: у этих возможностей отдельная цена или требования к конфиденциальности.

Пример целевого прогона:

```kotlin
jankHunter {
    profile.set(io.jankhunter.gradle.JankHunterProfile.TARGETED)
    enable(
        io.jankhunter.gradle.JankHunterFeature.JANK_STATS,
        io.jankhunter.gradle.JankHunterFeatureBundle.NETWORK_ALL,
        io.jankhunter.gradle.JankHunterFeatureBundle.DATABASE_ALL,
    )
}
```

## Возможности и наборы

Поддерживаются следующие значения `JankHunterFeature`:

| Область | Возможности |
| --- | --- |
| Интерфейс | `JANK_STATS`, `MAIN_LOOPER`, `INTERACTIONS`, `COMPOSE` |
| Сеть | `HTTP`, `WEBSOCKETS` |
| Базы данных | `SQLITE`, `ROOM` |
| Ввод-вывод | `RUNTIME_IO`, `BYTECODE_IO` |
| Асинхронная работа | `HANDLERS`, `EXECUTORS`, `COROUTINES`, `WORKERS` |
| Память и жизненный цикл | `LIFECYCLE_LEAKS`, `HEAP_DUMPS` |
| Код и сборка | `DI_ANALYSIS`, `CLASS_GRAPH`, `CALL_GRAPH`, `METHOD_COUNTERS` |
| Android | `ANDROID_COMPONENTS`, `BINDER_IPC` |
| Журналирование | `LOGGING` |

Готовые наборы `JankHunterFeatureBundle`: `UI_ALL`, `NETWORK_ALL`, `DATABASE_ALL`, `IO_ALL`, `CONCURRENCY_ALL`, `ANDROID_SYSTEM_ALL`.

```kotlin
jankHunter {
    profile.set(io.jankhunter.gradle.JankHunterProfile.BALANCED)
    enable(io.jankhunter.gradle.JankHunterFeatureBundle.NETWORK_ALL)
    disable(io.jankhunter.gradle.JankHunterFeature.LOGGING)
}
```

Явный выбор одной возможности имеет приоритет над выбором набора на том же уровне. Одновременное явное включение и выключение одной возможности на одном уровне считается ошибкой настройки.

## Варианты сборки

Настройки применяются по возрастанию приоритета:

```text
общие → тип сборки → вариант продукта → точный вариант
```

```kotlin
jankHunter {
    profile.set(io.jankhunter.gradle.JankHunterProfile.BALANCED)

    debug {
        enable(io.jankhunter.gradle.JankHunterFeature.CALL_GRAPH)
    }

    buildType("benchmark", io.jankhunter.gradle.JankHunterProfile.MINIMAL) {
        storageLimitMiB(8)
    }

    flavor("environment", "staging") {
        enable(io.jankhunter.gradle.JankHunterFeature.WEBSOCKETS)
    }

    variant("stagingDebug") {
        profile.set(io.jankhunter.gradle.JankHunterProfile.FULL)
        disable(io.jankhunter.gradle.JankHunterFeature.DI_ANALYSIS)
    }
}
```

`packages(...)` и `excludePackages(...)` накапливаются между уровнями. Для остальных значений побеждает наиболее конкретная настройка.

## Режим сбора, процессы и область внедрения

```kotlin
jankHunter {
    collection.set(io.jankhunter.gradle.JankHunterCollection.BALANCED)
    processes.set(io.jankhunter.gradle.JankHunterProcesses.MAIN_ONLY)
    scope.set(io.jankhunter.gradle.JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES)
    packages("com.example.feature", "com.example.data")
    excludePackages("com.example.generated", "com.example.di")
}
```

- `BALANCED` ограничивает ожидание и допускает контролируемый отказ от события при перегрузке.
- `EXACT` использует точный режим приёма, но всё равно соблюдает заданные пределы ожидания, чтобы не блокировать приложение неограниченно.
- `MAIN_ONLY` собирает только главный процесс.
- `ALL` включает все процессы приложения; для релизной сборки требуется отдельное подтверждение.
- `NAMESPACE_AND_PACKAGES` обрабатывает Android `namespace` и явно добавленные пакеты.
- `PACKAGES_ONLY` обрабатывает только пакеты из `packages(...)`.
- `WHOLE_APPLICATION` снимает пакетное ограничение для прикладных классов итогового приложения; системные и служебные пакеты по-прежнему исключаются.

В модуле приложения AGP предоставляет внедрению классы итогового приложения, а в библиотечном модуле - только классы проекта. Фактический фильтр Jank Hunter дополнительно ограничивается `scope`, `packages(...)` и `excludePackages(...)`.

## Настройка очереди и порогов

```kotlin
jankHunter {
    tuning.queueCapacity.set(32_768)
    tuning.methodFiltering.set(io.jankhunter.gradle.JankHunterMethodFilterMode.FILTER)

    tuning.thresholds.mainThreadStallMs.set(700)
    tuning.thresholds.ownerBlockMs.set(250)
    tuning.thresholds.slowHttpMs.set(1_000)
    tuning.thresholds.jankFrameMs.set(32)
    tuning.thresholds.uiWindowP95Ms.set(32)

    tuning.admission.mainThreadWaitMs.set(0)
    tuning.admission.backgroundWaitMs.set(5)
}
```

Значения времени указываются в миллисекундах. Вместимость очереди и основные пороги должны быть положительными, пределы ожидания - неотрицательными; это проверяется Gradle-задачей до сборки варианта.

## Хранилище и рост журналов

```kotlin
jankHunter {
    storageLimitMiB(16)
    growthAnalytics.set(true)
    deleteObsoleteLogs.set(false)
}
```

`storageLimitMiB(...)` ограничивает размер сегмента. После достижения предела среда выполнения запечатывает файл и продолжает тот же запуск в следующем сегменте. `unlimitedStorage()` снимает внутренний предел; в релизной сборке это требует `allowUnlimitedStorage()`.

`growthAnalytics` добавляет ограниченную историю роста журналов в JHLOG. `deleteObsoleteLogs` разрешает удаление файлов прежних несовместимых форматов при запуске; по умолчанию они сохраняются.

Встроенное хранилище можно заменить во время работы:

```kotlin
val result = JankHunter.switchBinaryStorage(customStorage)
JankHunter.switchBinaryStorage(null) // вернуться к встроенному хранилищу
```

Пользовательская реализация `JankHunterBinaryStorage` сама отвечает за архивный бюджет и очистку.

## Релизная диагностика

Релизные и похожие на них варианты требуют явного одобрения. Настройки не включаются скрыто: Gradle останавливает сборку, если отсутствует подтверждение конфиденциальности или файл с бюджетом производительности.

```kotlin
jankHunter {
    enabledBuildTypes.set(setOf("debug", "release"))

    buildType("release", io.jankhunter.gradle.JankHunterProfile.RELEASE_SAFE) {
        storageLimitMiB(16)
    }

    release {
        privacyReviewed()
        performanceBudget(project.file("docs/jankhunter-release-budget.md"))
    }
}
```

Файл бюджета должен существовать и содержать маркер:

```text
jankhunter_release_performance_budget_v1
```

При соответствующих настройках внутри `release { ... }` также требуются:

- `allowHeapDumps()` для `HEAP_DUMPS`;
- `allowSecondaryProcesses()` для `processes=ALL`;
- `allowUnlimitedStorage()` для неограниченного хранилища.

## Дампы памяти

Лёгкие сигналы удержания собираются возможностью `LIFECYCLE_LEAKS`. Для HPROF нужно явно включить `HEAP_DUMPS` и подтвердить проверку конфиденциальности:

```kotlin
jankHunter {
    debug {
        enable(io.jankhunter.gradle.JankHunterFeature.HEAP_DUMPS)
        privacyReviewed()
        tuning.heapDumps.minIntervalMs.set(600_000)
        tuning.heapDumps.maxCount.set(1)
        tuning.heapDumps.minRetainedAgeMs.set(30_000)
    }
}
```

Сначала записывается лёгкий сигнал. HPROF создаётся только после достижения минимального возраста и при условии, что объект всё ещё жив. Файл `retained-*.hprof` сохраняется рядом с журналами и автоматически обнаруживается утилитой, если передан тот же каталог.

Подтверждение конфиденциальности является проверкой Gradle и намеренно не записывается в AndroidManifest.

## Аннотации и ручная телеметрия

Доступны аннотации:

```kotlin
@JankHunterOwner("FeedRepository")
@JankHunterScreen("Feed")
@JankHunterOperation("Загрузка ленты", kind = JankHunterOperationKind.USER, budgetMs = 500)
@JankHunterIgnore
```

Они задают устойчивую атрибуцию для класса, функции или конструктора. `@JankHunterIgnore` исключает соответствующую область из внедрения.

Для динамического контекста используйте `JankHunterTelemetry`:

```kotlin
JankHunterTelemetry.setScreen("Feed")

JankHunterTelemetry.withContext("Feed", "FeedRepository") {
    JankHunterTelemetry.traceOperation("Обновление", budgetMs = 500) {
        repository.refresh()
    }
}

JankHunterTelemetry.counter("feed.refresh", 1)
JankHunterTelemetry.gauge("feed.items", items.size.toLong())
JankHunterTelemetry.watch(activity, "экран после onDestroy", "FeedActivity")
```

Также доступны ручные обёртки исполнителей, трассировка Compose и ввода-вывода, сеть через `JankHunterNetworkRuntime` и база данных через `JankHunterDatabaseTracing`.

## Автоматическое внедрение

В зависимости от включённых возможностей Gradle-плагин может:

- устанавливать `EventListener.Factory` для OkHttp и оборачивать `WebSocketListener`;
- измерять SQLite, Room и транзакции базы данных;
- оборачивать `Handler`, `Executor`, `Callable` и основные точки запуска корутин;
- связывать нажатия, экраны, Compose, WorkManager и жизненный цикл;
- считать вызовы `android.util.Log` и Timber без записи текста сообщений;
- собирать сведения о Service, BroadcastReceiver, AIDL и Binder;
- строить статический граф классов и агрегированный граф выполненных вызовов;
- измерять разрешённые целостные файловые операции.

Внедрение повторяемо, защищено служебной аннотацией и работает по принципу безопасного отказа: ошибка диагностического перехватчика не должна ломать исходную операцию приложения.

`CALL_GRAPH` записывает фактические связи вызывающий → вызываемый. `CLASS_GRAPH` создаёт только статические связи на этапе сборки. При отключённом `CALL_GRAPH` отчёт не может восстановить фактическую цепочку вызовов из одного статического графа.

## WorkManager

```kotlin
dependencies {
    implementation("io.jankhunter:jankhunter-workmanager:1.0.9")
    implementation("androidx.work:work-runtime:<версия-приложения>")
}
```

Модуль предоставляет `enqueueWithJankHunter(...)`, `JankHunterWorker` и `JankHunterCoroutineWorker`. Зависимость от WorkManager объявлена как `compileOnly`, поэтому приложение сохраняет выбранную им версию.

## Файлы JHLOG

По умолчанию файлы находятся здесь:

```text
context.filesDir/jankhunter/jh-session-log.YYYY-MM-DD.<run-id>.<index>.jhlog
```

Текущий формат - JHLOG `5.0.0`. Он хранит заголовок запуска и процесса, словарь, компактные записи событий, снимки качества и подтверждённые блоки. Сегменты одного запуска связаны контрольными суммами. Незавершённый хвост активного файла отделяется от повреждения, поэтому утилита может безопасно анализировать подтверждённую часть.

Копирование из отладочного приложения:

```bash
APP_ID=com.example.app
mkdir -p logs
adb exec-out run-as "$APP_ID" tar -C files/jankhunter -cf - . | tar -xf - -C logs
jankhunter inspect logs/*.jhlog --out report.html
```

## Артефакты Gradle-плагина

Для каждого включённого варианта создаются:

```text
build/generated/jankhunter/<variant>/artifact-metadata.json
build/generated/jankhunter/<variant>/class-graph.jsonl
build/generated/jankhunter/<variant>/instrumentation-diagnostics.jsonl
build/generated/jankhunter/<variant>/di-catalog.jsonl
build/generated/jankhunter/<variant>/android-components-catalog.jsonl
```

Часть файлов может быть пустой, если соответствующая возможность выключена. `artifact-metadata.json` содержит версию схемы, отпечаток пространства символов и эффективные параметры внедрения.

```bash
jankhunter inspect logs/*.jhlog \
  --artifacts-dir app/build/generated/jankhunter/debug \
  --mapping app/build/outputs/mapping/debug/mapping.txt \
  --out report.html
```

`--artifacts-dir` проверяет совместимость артефактов с журналом. Явные параметры `--class-graph`, `--instrumentation-diagnostics`, `--di-catalog` и `--android-components-catalog` имеют приоритет.

Каталог DI описывает связи Dagger, Hilt и поддерживаемые статические определения Koin только на этапе сборки. Эти связи не считаются вызовами времени выполнения, ссылками удержания или доказательством утечки и не влияют на оценку проблемы.

## Диагностические Gradle-задачи

```bash
./gradlew :app:jankHunterListProfiles
./gradlew :app:jankHunterCheckEnvironment
./gradlew :app:jankHunterPrintConfiguration
./gradlew :app:jankHunterPrintDebugConfiguration --details
./gradlew :app:jankHunterPrintDebugInstrumentationPlan
./gradlew :app:jankHunterValidateConfiguration
./gradlew :app:jankHunterCheckDebugReleaseParity
./gradlew :app:jankHunterCheckReleaseReadiness
./gradlew :app:jankHunterExportConfiguration
```

Экспорт эффективных настроек находится в `app/build/reports/jankhunter/configuration.json`. Проверка конкретного варианта автоматически подключается к его `pre<Variant>Build`.

## Нагрузка и безопасные настройки

- Начинайте с `BALANCED` и ограниченной области пакетов.
- Для короткой целевой проверки используйте `TARGETED` или точечные изменения поверх профиля.
- `CLASS_GRAPH` работает на этапе сборки и не добавляет события вызовов во время работы.
- `CALL_GRAPH`, `METHOD_COUNTERS`, `MAIN_LOOPER`, `COROUTINES` и `HEAP_DUMPS` требуют отдельной оценки затрат.
- События записываются асинхронно; при исчерпании бюджета ожидания они отклоняются с точным счётчиком причины, а не блокируют приложение неограниченно.
- Окончательное решение о допустимости нагрузки принимайте после прогона на представительном приложении и устройстве.

## Автоматическое подключение

Из корня Jank Hunter:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --profile balanced \
  --include-package com.example.app \
  --storage-limit-mib 16 \
  --verify
```

Скрипт использует `jankHunterVersion=1.0.9` из `android/gradle.properties`, добавляет плагин, `jankhunter-android-sdk:1.0.9`, локальный Maven-репозиторий и командную утилиту. Повторный запуск обновляет только принадлежащие скрипту блоки.

## Проверки

```bash
./gradlew detekt \
  :jankhunter-gradle-plugin:test \
  :jankhunter-runtime:testDebugUnitTest \
  :jankhunter-okhttp3:testDebugUnitTest \
  :jankhunter-workmanager:testDebugUnitTest \
  :sample-app:assembleDebug \
  --no-daemon

../scripts/gradle-plugin-smoke.sh
../scripts/android-e2e.sh
```

`gradle-plugin-smoke.sh` публикует Android `1.0.9` в изолированный Maven-репозиторий, собирает внешний проект в `debug` и `release` и проверяет повторное использование конфигурационного кэша.
