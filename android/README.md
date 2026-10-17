# Jank Hunter для Android

Android-часть Jank Hunter автоматически внедряет диагностические перехватчики на этапе сборки, собирает события во время работы приложения и сохраняет их в `.jhlog`. Актуальная версия SDK и Gradle-плагина - `1.0.13`.

```properties
jankHunterVersion=1.0.13
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
    id("io.jankhunter.android") version "1.0.13"
}

dependencies {
    implementation("io.jankhunter:jankhunter-android-sdk:1.0.13")
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
================JANK HUNTER 1.0.13 ENABLED================
```

## Инициализация и управление сбором

Статистика native executor (`active_count`, `pool_size`, `completed_task_count`) опрашивается отдельно от потока приложения: эти getters могут ожидать внутренний lock пула или выполнять код его подкласса. Канал ограничен одним потоком и одной ожидающей задачей, кеш содержит слабые ссылки максимум на 32 пула; запросы одного пула объединяются с интервалом не менее секунды. Метрика `pool_snapshot_age_ms` показывает возраст использованного снимка от начала опроса, поэтому долго выполнявшийся getter не делает старые данные свежими. До первого успешного опроса и при возрасте более двух секунд native-значения отсутствуют, а измерения очереди, ожидания и выполнения задач продолжаются. Зависший getter ограничивает доступность native-статистики, не переносится на вызывающий поток и не занимает канал записи или общей maintenance.

Тела конструкторов с exception handler, достижимым до завершения `this()`/`super()`, сохраняются без хуков; причина `constructor_preinitialization_handler` записывается в диагностику внедрения. Это предотвращает чтение неинициализированных локальных токенов из добавленного catch-all и повреждение stack maps. Конструктор с handler без доступного expanded frame также сохраняется консервативно, с причиной `constructor_handler_initialization_unknown`. Альтернативные пути инициализации `this` имеют причину `constructor_multiple_initialization_paths`: они определяются анализом исходного байткода, а не простым подсчётом всех вызовов конструкторов. Обычные конструкторы с post-super handler и созданием других объектов продолжают инструментироваться.

При остановке или истечении срока наблюдения WorkManager регистрация немедленно теряет право публикации и ссылки на epoch/writer. Callback отмены и закрытие handle снимка реестра выполняются асинхронно в общем отдельном канале с одним потоком и очередью максимум 64 задачи. Канал принадлежит runtime и сохраняется между реконфигурациями; отказ очереди не приводит к выполнению callback на вызывающем потоке. Отмена диагностического запроса при переполнении остаётся best effort и отмечается как collector failure; сама регистрация или запрос уже отключены. У закрытого запроса также очищается callback публикации: внешний источник, сохранивший callback, не удерживает через него старый collector/writer. Адаптер отменяет только диагностические запросы WorkManager.

`JankHunterRedactor.default()` скрывает email, UUID, длинные hex-идентификаторы и числовые сегменты пути. Входы длиннее 4096 символов пропускаются до обработки регулярными выражениями. Возвращённый redactor-ом `null` исключает значение метки, но сохраняет само событие; исходная строка обратно не подставляется. Это относится также к HTTP/WebSocket-меткам и имени процесса. Пользовательский redactor определяет собственную политику, а `JankHunterRedactor.none()` сохраняет исходные значения.

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
JankHunter.reconfigure("интервал записи") { builder -> builder.flushIntervalMs(2_000) }
JankHunter.flush()
JankHunter.initDiagnostics()
JankHunter.shutdown()
```

`setRuntimeEnabled` останавливает или возобновляет сбор без пересборки приложения. `reconfigure` применяет изменения к последней принятой конфигурации и сохраняет все незаданные параметры, включая выключенные флаги и выбранное хранилище. `flush()` отправляет уже принятые события на запись, но не запечатывает весь запуск. На UI и writer-потоке это асинхронный запрос; остальные вызывающие потоки используют один общий ограниченный дедлайн. Lifecycle/storage команды из writer callback, а reconfigure/storage также из startup callback, отклоняются до захвата control-locks; повторяйте их после возврата callback. Синхронные snapshot/export возвращают `null` как на UI, так и из writer callback; для отложенного захвата используйте асинхронные методы.

Каждый непрерывный сеанс сбора имеет собственное поколение (`epoch`). Отключение, перенастройка и остановка закрывают его; ротация файла сохраняет. HTTP, операции БД и Worker, начатые раньше, не завершаются событиями в новой сессии. Незавершённость, поздние или повторные завершения и отказы учитываются раздельно.

При повторном включении восстанавливается наблюдение уже открытого экрана. Если старый consumer не вышел к deadline, допускается одно новое изолированное поколение; при занятых двух слотах последнее запрошенное включение ждёт освобождения владельца. Пропущенный сбор отражается в качестве данных.

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

Периодические снимки потоков включаются тем же Gradle DSL:

```kotlin
jankHunter {
    enable(io.jankhunter.gradle.JankHunterFeature.THREAD_SAMPLING)
    tuning.threads.intervalMs.set(5_000L)
}
```

По умолчанию снимки потоков выключены, в том числе в профиле `FULL`; для отключения используйте `disable(io.jankhunter.gradle.JankHunterFeature.THREAD_SAMPLING)`. Минимальный интервал — 500 мс. Плагин записывает настройки в манифест приложения; обновления runtime-конфигурации сохраняют их, если явно не изменяют эти поля.

### Причины проблем с многопоточностью

Причинная диагностика включается через тот же enum и `enable`/`disable`:

```kotlin
jankHunter {
    enabled.set(true)
    enabledBuildTypes.set(setOf("debug", "release"))
    enable(io.jankhunter.gradle.JankHunterFeatureBundle.CONCURRENCY_ALL)
    tuning.concurrency.detailedEnabled.set(false)
    tuning.concurrency.maxTrackedEntities.set(4096)
    release {
        privacyReviewed()
        performanceBudget(project.file("docs/jankhunter-release-budget.md"))
    }
}
```

`CONCURRENCY_ALL` включает `HANDLERS`, `EXECUTORS`, `COROUTINES`, `WORKERS`,
`THREAD_SAMPLING` и `CONCURRENCY_DIAGNOSTICS`. Можно включить только последний
feature, если нужны причинные события без периодических снимков потоков. Настройка
доступна и в debug, и в release; удалённая конфигурация может отключать уже
инструментированную возможность, но не добавлять отсутствующие в сборке хуки.

`CONCURRENCY_DIAGNOSTICS` сохраняет исходный Runnable/Callable обычного Executor:
он наблюдает постановку, результат передачи, отмену и shutdown. Точные границы
выполнения получают уже включённые controlled-декораторы `EXECUTORS` либо явный
`JankHunterTrackedExecutor(delegate)`. Это позволяет сохранить договорённости
обычного Executor о `remove`, `shutdownNow` и rejection handler.

Новые журналы содержат точные идентификаторы работы и инструментированные места
вызовов. Отчёт начинает анализ с конкретного последствия, метода, доступной строки
и следующего действия. Код приложения определяется по происхождению классов в
сборке, включая локальные модули, а не по совпадению пакета с `applicationId`.
Инициатор и библиотека, управляющая исполнением, показываются отдельно.

Если запуск корутины не удалось точно связать с её state machine, отчёт показывает
первое наблюдение выполнения отдельно и не выдаёт его за создание. Повторные
`enqueue` одного UUID из разных мест кода делают инициатора Worker неоднозначным:
отчёт сохраняет неизвестное происхождение вместо выбора последнего вызова.

Подробный режим включается отдельно: `tuning.concurrency.detailedEnabled.set(true)`.
Он добавляет явно отслеживаемые resource leases. Например:

```kotlin
JankHunterResources.withLock(lock) {
    JankHunterResources.use(outputStream) { originalStream ->
        originalStream.flush()
    }
}
```

`withMonitor` использует исходный объект монитора. Для других поддержанных API,
включая WakeLock, доступны `beginAcquire`, `acquired`, `acquireFailed` и `released`:
границы вызывают непосредственно до/после реального API. `released` отмечают только
после успешного освобождения. `boundToCurrentScope=true` явно обещает освободить
lease до конца текущей работы. При передаче ресурса вызывают
`transferToCurrentScope` в новом scope либо `transferOutsideScope` при возврате
открытого ресурса вызывающему коду. Обычный I/O сам по себе не устанавливает
владение ресурсом. Heap процесса не распределяется между потоками.

`use` получает уже существующий `AutoCloseable`, поэтому отмечает начало его
наблюдения и фактическое закрытие. Место открытия и полный возраст ресурса из
этого вызова не выводятся. Если важно зафиксировать именно открытие, вызовите
`beginAcquire` перед реальным API, затем `acquired` либо `acquireFailed`.
`transferToCurrentScope` без текущей отслеживаемой задачи сохраняет прежнего
владельца; снять обязательство освобождения можно явно через `transferOutsideScope`.

Запрос отмены, interrupt и shutdown не означают завершение работы; retry не означает
провал WorkManager-цепочки. Работа в момент записи и открытый unbound lease не
объявляются утечками. Обычные Handler callbacks сохраняют identity; без явной
безопасной привязки отчёт не придумывает post→dispatch. Для native-потоков доступны
снимки общей нагрузки; происхождение и Java/Kotlin lifecycle не восстанавливаются.
В старых журналах нет новых причинных связей: для их получения нужно записать
сценарий заново. Формат 5.2 требует CLI из того же checkout; новая CLI также читает
журналы 5.0/5.1.

Для точной связи каждой постановки Handler с dispatch используют отдельный API:

```kotlin
val trackedHandler = JankHunterTrackedHandler(handler)
val post = trackedHandler.postDelayed(callback, 1_000L)
post?.requestRemoval()
```

Каждый post получает собственный handle, даже при повторном callback. Запрос удаления
не подтверждает остановку уже запущенного callback. Задержка Handler записывается
как аргумент API: его часы uptime не подменяются elapsedRealtime через deep sleep.

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

Финальная диагностика при остановке выполняется в фоне. Вызывающий поток ждёт только до общего deadline; уже начатый платформенный dump может закончиться позже. Одновременно разрешён один dump. Отчёт различает удержание после задержки, после запрошенного GC и путь к экземпляру класса в HPROF: совпадение класса не устанавливает идентичность наблюдаемого объекта.

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

- устанавливать `EventListener.Factory` для OkHttp, наблюдать чтение первого байта ответа в поддерживаемом транспорте и оборачивать `WebSocketListener`;
- измерять SQLite, Room и транзакции базы данных;
- наблюдать постановку через `Handler`, оборачивать `Executor`, `Callable` и основные точки запуска корутин;
- связывать нажатия, экраны, Compose, WorkManager и жизненный цикл;
- считать вызовы `android.util.Log` и Timber без записи текста сообщений;
- собирать сведения о Service, BroadcastReceiver, AIDL и Binder;
- строить статический граф классов и агрегированный граф выполненных вызовов;
- измерять разрешённые целостные файловые операции.

Внедрение повторяемо, защищено служебной аннотацией и работает по принципу безопасного отказа: ошибка диагностического перехватчика не должна ломать исходную операцию приложения.

`CALL_GRAPH` записывает фактические связи вызывающий → вызываемый. `CLASS_GRAPH` создаёт только статические связи на этапе сборки. При отключённом `CALL_GRAPH` отчёт не может восстановить фактическую цепочку вызовов из одного статического графа. Явно размеченные интервалы Compose, Room и Worker могут продолжать записываться через общий транспорт графа: они показывают длительность операции, а не цепочку вызовов.

В очереди `Handler` остаётся исходный `Runnable`, поэтому `removeCallbacks` и `hasCallbacks` сохраняют поведение и из неинструментированного кода. Если конкретную постановку нельзя достоверно связать с выполнением, контекст остаётся `unknown`, а число таких постановок выводится отдельно от полноты доставки событий.

Обычная executor-обёртка сохраняет безопасные строковые и числовые метаданные отправителя для допустимых повторных вызовов делегатом. Scheduled-обёртка освобождает служебные ссылки после завершения или отмены. Отсутствующий контекст не заменяется более поздним экраном.

## Семантика измерений

| Измерение | Что означает |
| --- | --- |
| Первый байт HTTP (`ttfb_ms`) | Время от начала первой отправки заголовков Call до чтения первого байта его ответа из транспорта, до HTTP-парсера. Для HTTP/2 учитывается конкретный stream. Это время чтения приложением, а не прихода пакета в ядро. |
| Границы TTFB | Включает отправку тела и повторы до первого ответа; первый промежуточный или redirect-ответ тоже считается. Интервалы request/response могут пересекаться с TTFB, их нельзя складывать как независимые фазы. |
| Известность TTFB | Ноль является измерением только при подтверждённом наблюдении. Отсутствующие transport hooks, cache-only ответ или ошибка измерения дают `unknown`. Одного `EventListener` недостаточно для точного первого байта. |
| HTTP body bytes | Сумма наблюдавшихся тел всех обменов одного Call, включая повторы и перенаправления; без заголовков и транспортных накладных расходов. Полнота каждой суммы отмечается отдельно. |
| UID-трафик | RX/TX принадлежат UID и могут быть общими для нескольких процессов. CLI объединяет подтверждённые диапазоны счётчиков, не складывая их повторно; недоступный счётчик не считается нулём. |
| Запланированная задача | `scheduled_delay_ms` - задержка приложения; `scheduled_lateness_ms` - опоздание относительно срока. Fixed-rate считается от расписания, fixed-delay - от завершения предыдущего запуска. Обычный Executor сохраняет `wait_ms`. |
| Отключённый fixed-delay | Часы завершения читаются даже при выключенной телеметрии, чтобы следующий запуск после включения получил правильный срок. Остальные измерения отключённой обёртки обходятся. |
| Пауза главного потока | Один инцидент имеет состояния `ongoing`, `recovered`, `interrupted`. До восстановления и при прерывании длительность является нижней границей. Deep sleep не прибавляется к stall. |
| Транзакции БД | Rollback переносится подходящим предкам той же БД. Связь сохраняется глубже 16 уровней; стек растёт по активной глубине и освобождает увеличенное хранилище после полного выхода. |

Периодический flush использует активное монотонное время: сон устройства не расходует интервал. Явный flush и новые события уведомляют consumer. Суммы gauge хранятся точно в расширенном формате; исторически насыщенные суммы старых журналов восстановить нельзя.

## WorkManager

```kotlin
dependencies {
    implementation("io.jankhunter:jankhunter-workmanager:1.0.13")
    implementation("androidx.work:work-runtime:<версия-приложения>")
}
```

Модуль предоставляет `enqueueWithJankHunter(...)`, `JankHunterWorker` и `JankHunterCoroutineWorker`. Зависимость от WorkManager объявлена как `compileOnly`, поэтому приложение сохраняет выбранную им версию.

Для наблюдения подтверждённого входа в foreground вызывайте в своих наследниках
`setJankHunterForegroundAsync(info)` у `JankHunterWorker` либо suspending
`setJankHunterForeground(info)` у `JankHunterCoroutineWorker`. Событие записывается лишь
после успешного завершения соответствующего WorkManager API. При завершении попытки
сохраняется граница наблюдения, но фактическое время выхода из foreground неизвестно.
Вызовы `setForegroundAsync`/`setForeground` напрямую этим адаптером не перехватываются.

После успешной операции постановки SDK асинхронно проверяет наличие UUID. `REGISTERED_OBSERVED` означает подтверждённую запросом регистрацию, а не доказанную новую вставку. При проигнорированном `KEEP` не создаётся ложный `ENQUEUED`. Наблюдение не задаёт время ожидания запуска и может прийти после старта или завершения Worker; polling не используется.

## Файлы JHLOG

По умолчанию файлы находятся здесь:

```text
context.filesDir/jankhunter/jh-session-log.YYYY-MM-DD.<run-id>.<index>.jhlog
```

Текущий формат - JHLOG `5.1.0`; актуальный CLI также читает `5.0.0`. Он хранит заголовок запуска и процесса, словарь, компактные записи событий, снимки качества и подтверждённые блоки. Сегменты одного запуска связаны контрольными суммами. Незавершённый хвост активного файла отделяется от повреждения, поэтому утилита может анализировать подтверждённую часть.

Новые обязательные возможности формата включают точную сумму gauge, семантику первого байта, происхождение UID-трафика и состояние HTTP-сбора. Reader без их поддержки завершает анализ ошибкой. Старые значения не переименовываются в новые измерения: например, legacy TTFB не участвует в статистике фактически наблюдённого первого байта. Канонический контракт хранится в [wire/jhlog-v5.json](../wire/jhlog-v5.json).

При неполном crash-drain или другом неизвестном объёме потерь CLI сохраняет числовое `diagnostic_completeness_percent=-1`. Подробности сбора показаны в «Математический анализ → Качество данных». Полнота доставки и покрытие Handler/HTTP-измерений - разные характеристики.

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
- События записываются асинхронно; при исчерпании бюджета ожидания фиксируется причина отказа. Если объём отсутствующих данных неизвестен, отчёт не выдаёт его за точное число потерянных событий.
- Producer-хранилище `CALL_GRAPH` имеет квоту 8 МиБ на поколение и не более двух поколений одновременно. Это не ограничение общей памяти SDK: writer, consumer-таблицы и другие структуры учитываются отдельно. Пропущенные участки не соединяются выдуманными связями.
- Окончательное решение о допустимости нагрузки принимайте после прогона на представительном приложении и устройстве.

## Публикация в Maven Local

Из каталога `android`:

```bash
./gradlew publishToMavenLocal --no-daemon --console=plain
./gradlew -p jankhunter-gradle-plugin publishToMavenLocal --no-daemon --console=plain
```

Нужны обе команды: composite Gradle-плагин и marker публикуются отдельно от SDK. По умолчанию используется `~/.m2/repository`; для другого каталога передайте одинаковый `-Dmaven.repo.local=/абсолютный/путь` обеим командам. В приложении подключите `mavenLocal()` в `pluginManagement.repositories` и `dependencyResolutionManagement.repositories` либо укажите этот каталог явно.

При перезаписи той же версии `1.0.13` выполните Gradle sync или сборку потребителя с `--refresh-dependencies`. CLI имеет независимую версию `1.0.13` и должен быть пересобран из согласованного checkout.

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

Скрипт использует `jankHunterVersion=1.0.13` из `android/gradle.properties`, добавляет плагин, `jankhunter-android-sdk:1.0.13`, локальный Maven-репозиторий и командную утилиту. Перед первым запуском подготовьте отдельную публикацию Gradle-плагина в каталог интегратора по [инструкции scripts/README.md](../scripts/README.md#подготовка-gradle-плагина). Повторный запуск обновляет только принадлежащие скрипту блоки. `--verify` проверяет разрешение Gradle-задач; приложение нужно собрать отдельно.

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

`gradle-plugin-smoke.sh` публикует Android `1.0.13` в изолированный Maven-репозиторий, собирает внешний проект в `debug` и `release` и проверяет повторное использование конфигурационного кэша.

### Наблюдение расписаний WorkManager

Плагин фиксирует вычисленные аргументы публичных конструкторов
`PeriodicWorkRequest.Builder` (`Class`/`KClass`, `TimeUnit`/`Duration`) и
`setInitialDelay` после успешного исходного вызова. Код приложения не
перевычисляется. Явное окно отделено от конструктора без отдельного окна;
ненаблюдаемые параметры остаются неизвестными. Хранятся ограниченные
примитивные подсказки, без сильного удержания builder и запроса. При
конкуренции tracker не ждёт блокировку и не использует устаревшие подсказки.

Для действующего состояния отдельно подключается опциональный артефакт
`io.jankhunter:jankhunter-workmanager` и источник
`WorkManagerRegistrySource` через `workerRegistrySource(...)` конфигурации SDK.
Передавайте уже штатно инициализированный `WorkManager`; диагностический
сбор не должен инициировать его запуск на потоке интерфейса. Источник
сохраняйте между опросами, чтобы работала ротация ограниченной выборки.
Запросы реестра выполняет фоновый collector. Публичный API WorkManager
материализует результат запроса до ограничения в 128 преобразованных строк;
ограничение JH не является пагинацией самой базы WorkManager.

Новые поля записываются в JHLOG **5.3.0** и требуют обновлённого CLI.
Наблюдение аргументов не подтверждает принятие запроса: KEEP может сохранить
предыдущее задание. Раннее допустимое время из WorkInfo не является дедлайном;
заявленное требование сети не доказывает, что сеть отсутствовала при задержке.
