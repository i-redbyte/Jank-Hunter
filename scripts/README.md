# Служебные скрипты Jank Hunter

Скрипты подключают Android `1.0.9` к приложению, проверяют Gradle-плагин и сквозной сценарий, измеряют производительность и маршрутизируют проверки монорепозитория.

## Состав

| Файл | Назначение |
| --- | --- |
| `integrate-android-project.sh` | Публикует Jank Hunter локально и подключает его к существующему Android-проекту. |
| `gradle-plugin-smoke.sh` | Собирает внешний проект-потребитель и проверяет контракт плагина и SDK. |
| `android-e2e.sh` | Запускает демонстрационный сценарий на устройстве, копирует журнал и создаёт отчёт. |
| `validate-android-e2e.py` | Проверяет итоговый JSON по версионируемому контракту. |
| `performance-baseline.py` | Создаёт и сравнивает локальные снимки производительности. |
| `jank_hunter_ci.py` | Определяет область изменений и запускает нужную группу проверок монорепозитория. |
| `run-runtime-arm64-soak.sh` | Многократно запускает проверку графа среды выполнения на устройстве ARM64. |
| `sync-github-master-to-gitlab.sh` | Синхронизирует внешний GitHub-проект с поддеревом GitLab и обновляет версию. |
| `test_*.py` | Модульные и регрессионные проверки скриптов. |

Справка:

```bash
./scripts/integrate-android-project.sh --help
./scripts/gradle-plugin-smoke.sh --help
./scripts/android-e2e.sh --help
python3 -B scripts/performance-baseline.py --help
python3 -B scripts/jank_hunter_ci.py --help
```

## Требования

- Bash и стандартные Unix-утилиты;
- Perl для интегратора;
- Go для сборки командной утилиты;
- JDK 17 или новее для Android-сборки;
- Android SDK и установленная версия Build Tools;
- Python 3 для сценариев проверки;
- `adb` и устройство либо эмулятор только для проверок на Android.

Перед изменением реального приложения рекомендуется проверить чистоту Git и сначала использовать `--dry-run`. Интегратор не создаёт коммиты в целевом проекте.

## Подключение к Android-проекту

Минимальный запуск из корня Jank Hunter:

```bash
./scripts/integrate-android-project.sh ~/work/MyApp
```

Рекомендуемый явный запуск:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --profile balanced \
  --collection balanced \
  --processes main-only \
  --scope namespace-and-packages \
  --include-package com.example.app \
  --exclude-package com.example.generated \
  --storage-limit-mib 16 \
  --verify
```

Скрипт читает `jankHunterVersion=1.0.9` из `android/gradle.properties` текущей рабочей копии и использует эту версию для плагина и `jankhunter-android-sdk`.

### Что изменяется

По умолчанию интегратор:

1. Проверяет структуру целевого проекта и выбранные модули.
2. Публикует Android-артефакты в `<target>/.jankhunter/maven`.
3. Собирает утилиту и копирует её в `<target>/.jankhunter/bin/jankhunter`.
4. Добавляет локальный Maven-репозиторий в `pluginManagement` и `dependencyResolutionManagement`.
5. Добавляет плагин `io.jankhunter.android` версии `1.0.9`.
6. Добавляет единую зависимость `io.jankhunter:jankhunter-android-sdk:1.0.9`.
7. Создаёт управляемый блок `jankHunter { ... }` только для явно переданных настроек.
8. При необходимости создаёт или обновляет `sdk.dir` в `local.properties`.
9. Добавляет локальные каталоги и `local.properties` в `.gitignore`.

Перед записью изменяемые файлы копируются в:

```text
<target>/.jankhunter-backups/YYYYMMDD-HHMMSS-<pid>.<random>/
```

Запись выполняется атомарно. Ошибка после начала транзакции, включая неуспешный `--verify`, восстанавливает файлы проекта из резервной копии. Уже опубликованный локальный Maven-репозиторий, утилита и каталоги сборки могут остаться для диагностики.

### Выбор модуля

Без `--module` скрипт ищет модули с Android application-плагином и ранжирует их как запускаемые приложения. В многомодульном проекте лучше задать модули явно:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --module :demo
```

`--module`, `--include-package`, `--exclude-package`, `--enable-feature`, `--disable-feature` и `--build-type` можно повторять. Для пакетов и типов сборки поддерживаются списки через запятую.

### Профили и возможности

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --module :app \
  --profile targeted \
  --collection exact \
  --enable-feature jank-stats \
  --enable-feature network-all \
  --enable-feature database-all \
  --disable-feature logging \
  --include-package com.example.app
```

Профили: `minimal`, `balanced`, `full`, `release-safe`, `targeted`.

Возможности:

```text
jank-stats, main-looper, sqlite, room, http, websockets,
runtime-io, bytecode-io, di-analysis, handlers, executors,
coroutines, interactions, lifecycle-leaks, logging, class-graph,
call-graph, compose, workers, android-components, binder-ipc,
method-counters, heap-dumps
```

Наборы:

```text
ui-all, network-all, database-all, io-all,
concurrency-all, android-system-all
```

Одну возможность нельзя одновременно передать в `--enable-feature` и `--disable-feature`. Конфликт обнаруживается до публикации и записи файлов.

Сохранились краткие совместимые параметры:

| Параметр | Эквивалент |
| --- | --- |
| `--okhttp` | `--enable-feature http` |
| `--websockets` | `--enable-feature websockets` |
| `--runtime-call-graph` | `--enable-feature call-graph` |
| `--analyze-di` | `--enable-feature di-analysis` |

Для каждого есть отрицательная форма `--no-...`.

### Полный перечень основных параметров

| Параметр | Действие |
| --- | --- |
| `PATH`, `--target PATH` | Корень целевого Android-проекта; обязателен. |
| `--jankhunter PATH` | Рабочая копия Jank Hunter с каталогами `android/` и `cli/`. |
| `--module :app` | Изменяемый Android-модуль; можно повторять. |
| `--profile NAME` | Один из пяти профилей. |
| `--collection balanced\|exact` | Режим приёма событий. |
| `--processes main-only\|all` | Главный или все процессы приложения. |
| `--scope namespace-and-packages\|packages-only\|whole-application` | Область ASM-внедрения. |
| `--enable-feature NAME` | Включить возможность или набор. |
| `--disable-feature NAME` | Выключить возможность или набор. |
| `--include-package PREFIX` | Добавить пакет в область внедрения. |
| `--exclude-package PREFIX` | Исключить пакет из области внедрения. |
| `--build-type TYPE` | Включённый тип сборки. |
| `--auto-init`, `--no-auto-init` | Управлять автоматической инициализацией. |
| `--growth-analytics`, `--no-growth-analytics` | Управлять историей роста журналов. |
| `--delete-obsolete-logs`, `--keep-obsolete-logs` | Удалять или сохранять старые форматы. |
| `--storage-limit-mib N` | Ограничить размер сегмента положительным числом МиБ. |
| `--unlimited-storage` | Снять внутренний предел размера. |
| `--maven-dir PATH` | Локальный Maven-репозиторий внутри целевого проекта. |
| `--cli-dir PATH` | Каталог утилиты внутри целевого проекта. |
| `--android-sdk PATH` | Явный путь к Android SDK. |
| `--android-build-tools VERSION` | Явная установленная версия Build Tools. |
| `--verify` | Проверить разрешение Gradle-задач после записи. |
| `--dry-run` | Выполнить предварительную проверку и показать план без записи. |
| `--skip-publish` | Не публиковать Android-артефакты. |
| `--skip-cli-build` | Не собирать и не копировать утилиту. |
| `--skip-local-properties` | Не менять `local.properties`. |
| `--no-gitignore` | Не менять `.gitignore`. |

Старые имена `--max-session-log-size-mib` и `--no-session-log-size-limit` сохранены как совместимые псевдонимы для новых `--storage-limit-mib` и `--unlimited-storage`.

### Повторный запуск

Скрипт владеет только блоками между своими маркерами `BEGIN/END`. Ручные `jankHunter { ... }`, зависимости, репозитории и комментарии вне этих блоков сохраняются.

Если не передана ни одна настройка DSL, существующий управляемый блок сохраняется без изменений. Если передан хотя бы один параметр профиля, сбора, процессов, области, возможности, пакета, типа сборки, инициализации, аналитики или хранилища, блок целиком пересобирается только из текущего явного набора. Поэтому при изменении настройки повторяйте все желаемые явные параметры.

Повторный запуск с одинаковыми аргументами не должен менять `settings.gradle(.kts)`, `build.gradle(.kts)` и каталог версий.

### Безопасность путей

`--maven-dir` и `--cli-dir` должны быть относительными путями внутри целевого проекта. Переход через `..`, выход через символьную ссылку, повисшая ссылка или файл вместо ожидаемого каталога отклоняются. Неоднозначные объявления плагина, повреждённые маркеры и небезопасные пути останавливают работу до изменений.

## Проверка Gradle-плагина внешним потребителем

```bash
./scripts/gradle-plugin-smoke.sh
```

Сценарий:

1. Публикует плагин и Android-артефакты `1.0.9` в изолированный Maven-репозиторий.
2. Проверяет, что `jankhunter-android-sdk` транзитивно содержит среду выполнения, аннотации и поддержку OkHttp.
3. Создаёт внешний проект с приложением и библиотечными модулями.
4. Собирает `debug` и `release`, включая R8.
5. Проверяет эффективную настройку, артефакты внедрения, манифест, граф классов, DI, OkHttp/WebSocket и WorkManager.
6. Повторяет сборку и требует повторного использования конфигурационного кэша.

Переменные окружения:

| Переменная | Назначение |
| --- | --- |
| `SMOKE_JAVA_HOME` | JDK 17 или новее. |
| `SMOKE_AGP_VERSION` | Версия AGP внешнего проекта. |
| `SMOKE_COMPILE_SDK` | Версия `compileSdk` и `targetSdk`. |
| `ANDROID_BUILD_TOOLS_VERSION` | Установленная версия Build Tools. |
| `SMOKE_CONFIGURATION_CACHE=0` | Отключить двойную проверку конфигурационного кэша. |
| `SMOKE_WORK_DIR` | Родитель уникального каталога прогона, сохраняемого для анализа. |
| `KEEP_SMOKE_DIR=1` | Сохранить автоматически созданный временный каталог. |

## Сквозная проверка Android

```bash
./scripts/android-e2e.sh
```

При нескольких устройствах:

```bash
adb devices
./scripts/android-e2e.sh --serial emulator-5554
```

Основные параметры:

| Параметр | Значение по умолчанию |
| --- | --- |
| `--out-dir PATH` | `reports/android-e2e` |
| `--serial SERIAL` | Единственное устройство в состоянии `device` |
| `--instrumentation-diagnostics PATH` | Не передаётся |
| `--contract PATH` | `scripts/contracts/android-sample-e2e.json` |

Сценарий собирает APK, проверяет идентификаторы пакетов, запускает ровно `SampleEndToEndLogTest`, копирует `.jhlog` и HPROF через `run-as`, удаляет тестовые пакеты, создаёт JSON и HTML и применяет строгий контракт. Потери, повреждение, отсутствие обязательных событий или несоответствие метрик завершают прогон ошибкой.

Выходной каталог очищается только при наличии точного маркера владения. Чужой непустой каталог, домашний каталог, корень репозитория и символьная ссылка не очищаются.

## Контракт производительности

Создание эталона и кандидата:

```bash
python3 -B scripts/performance-baseline.py capture \
  --out benchmarks/results/reference.json

python3 -B scripts/performance-baseline.py capture \
  --out benchmarks/results/candidate.json
```

Сравнение:

```bash
python3 -B scripts/performance-baseline.py check \
  --reference benchmarks/results/reference.json \
  --candidate benchmarks/results/candidate.json
```

Текущие версии схем: снимок `12`, критерии `4`, синтетический набор `4`. Штатный контракт находится в `benchmarks/acceptance.json`. Подробности - в [`../benchmarks/README.md`](../benchmarks/README.md).

## Маршрутизация CI

```bash
python3 -B scripts/jank_hunter_ci.py scope
python3 -B scripts/jank_hunter_ci.py run static-analysis
python3 -B scripts/jank_hunter_ci.py run assemble
python3 -B scripts/jank_hunter_ci.py run unit-tests
```

Команда `scope` определяет, достаточно ли проверок поддерева Jank Hunter или требуется общий конвейер монорепозитория. При отсутствии достоверных сведений о сравнении изменений выбирается безопасный общий вариант.

## Длительная проверка ARM64

```bash
SOAK_ITERATIONS=20 ADB_BIN=adb ./scripts/run-runtime-arm64-soak.sh
```

Требуется ровно одно авторизованное устройство с ABI `arm64-v8a`. Скрипт запускает `RuntimeGraphArtTest` указанное число раз.

## Синхронизация GitHub и GitLab

```bash
./scripts/sync-github-master-to-gitlab.sh --dry-run
./scripts/sync-github-master-to-gitlab.sh --version 1.0.9 --branch <ветка>
```

Сценарий требует чистое состояние отслеживаемых файлов, копирует ветку `master` внешнего проекта в поддерево `jank-hunter/`, обновляет версию, создаёт коммит и отправляет выбранную ветку GitLab. Без `--version` увеличивается номер исправления. Сначала всегда используйте `--dry-run`.

## Модульные проверки скриптов

```bash
python3 -m unittest \
  scripts.test_scripts \
  scripts.test_gradle_plugin_repositories \
  scripts.test_jank_hunter_ci \
  scripts.test_performance_baseline \
  scripts.test_ci_routing
```

Проверки используют временные каталоги и подменённые внешние команды. Они не запускают устройство и не выполняют полный набор измерений производительности.

## Частые проблемы

Если не найден Android SDK, передайте `--android-sdk` интегратору или задайте `ANDROID_HOME`/`ANDROID_SDK_ROOT`.

Если Gradle использует неподходящую Java, задайте `SMOKE_JAVA_HOME` для проверки внешнего потребителя и проверьте JVM, выбранную Gradle Wrapper целевого проекта.

Если сквозная проверка не видит устройство, выполните `adb devices`; учитываются только строки со статусом `device`. При нескольких устройствах укажите `--serial` или `ANDROID_SERIAL`.

Если интегратор выбрал не тот модуль, передайте `--module :нужный-модуль` явно. Для просмотра плана без сборки и записи используйте:

```bash
./scripts/integrate-android-project.sh \
  --target ~/work/MyApp \
  --dry-run
```
