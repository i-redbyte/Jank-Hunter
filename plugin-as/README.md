# Jank Hunter for Android

В этой директории находится плагин IntelliJ Platform для запуска локального Jank Hunter CLI из Android Studio или IntelliJ IDEA.

Плагин умеет:

- запускать `inspect`, `compare`, `problems`, `scorecard`, `sample` и `version` из tool window;
- работать как обычная Run Configuration IDE;
- запускать Gradle-сценарии для sample app, connected tests и сборки Jank Hunter artifacts;
- находить Android Gradle plugin artifacts: `owner-map`, `class-graph`, `instrumentation-diagnostics`, `mapping.txt`;
- работать с ADB: искать устройства, подтягивать `.jhlog` с устройства и запускать сценарий `collect logs -> inspect`;
- показывать `problems.csv`/`problems.json` в таблице с фильтрами, группировкой и переходом в исходники;
- открывать HTML-отчеты внутри IDE или в браузере по умолчанию;
- хранить project-level профили в `.jankhunter/plugin.json`.

## Требования

- IntelliJ IDEA 2026.1.3 или другая IDE на IntelliJ Platform с JBR 21.
- Встроенный Gradle wrapper из этой директории. Системный Gradle не нужен.
- Собранный бинарник Jank Hunter CLI, обычно `../cli/bin/jankhunter`, или команда `jankhunter`, доступная через `PATH`.

## Сборка

```bash
./gradlew buildPlugin
```

ZIP-файл плагина будет создан здесь:

```text
build/distributions/
```

## Запуск в sandbox IDE

```bash
./gradlew runIde
```

По умолчанию Gradle использует локальную IDE по пути:

```text
/Applications/IntelliJ IDEA.app
```

Если этого пути нет, сборка использует зависимость IntelliJ IDEA `2026.1.3`.

Чтобы запустить sandbox на другой IDE, передайте путь явно:

```bash
./gradlew runIde -PlocalIdePath="/Applications/Android Studio.app"
```

## Настройка CLI

Сначала соберите CLI:

```bash
cd ../cli
make build
```

Затем откройте tool window `Jank Hunter`. Если плагин не нашел CLI автоматически, укажите путь в поле `CLI`:

```text
../cli/bin/jankhunter
```

Кнопка `Check CLI` проверяет наличие CLI и выводит `jankhunter version`. Кнопка `Build CLI` запускает `make build` для локального CLI.

## Проверки

Перед установкой или публикацией полезно прогнать:

```bash
./gradlew test buildPlugin
./gradlew verifyPlugin
```

`verifyPlugin` проверяет собранный ZIP против локальной IntelliJ IDEA из `localIdePath`.
