# Jank Hunter для Android Studio

Плагин превращает локальные `.jhlog` и HPROF в HTML-отчёты Jank Hunter без ручной сборки CLI-команд. Источник логов не обязан относиться к приложению или проекту, открытому в Android Studio.

## Интерфейс

Плагин открывает отдельное modeless-окно `Jank Hunter`, поэтому простой и расширенный режимы не зависят от ширины боковой панели IDE. Окно можно перемещать, изменять его размер и использовать параллельно с редактором. Открыть его можно через `Tools → Jank Hunter` или `Control Alt J`.

### Отчёт

Простой режим по умолчанию:

1. Выберите папку с логами.
2. Отметьте один, несколько либо все `.jhlog`.
3. При необходимости выберите найденный HPROF или heap-evidence JSON.
4. Нажмите `Сгенерировать отчёт`.

Файлы сортируются по дате и числовому индексу canonical session-имени. После успешной генерации их fingerprints сохраняются; при следующем сканировании новые файлы выбираются автоматически. Если история пуста, выбираются все найденные логи.

Найденный HPROF никогда не подключается к анализу скрыто: при единственном кандидате доступна кнопка `Подставить HPROF`, при нескольких дампах нужный файл выбирается через диалог. Это позволяет не запускать тяжёлый heap-анализ случайно.

### Сравнение

Экран `Сравнение` принимает две независимые группы:

- `Было · baseline`: один или несколько логов и опциональный HPROF/evidence;
- `Стало · candidate`: один или несколько логов и опциональный HPROF/evidence.

Группы не должны пересекаться. Результатом является единый HTML compare-отчёт.

### Расширенный режим

Расширенный режим наследует текущий выбор простого экрана и предоставляет:

- автоматический поиск каталога Gradle-артефактов и ручной выбор `artifacts-dir`, `mapping`, `class-graph`, instrumentation diagnostics и DI catalog;
- фильтры route, screen, owner и class;
- presentation mode и animated background;
- скрытый из текущего UI экспорт Problems CSV как задел для будущего вторичного действия;
- оценку качества сравнения baseline/candidate;
- путь к CLI, preview команды и диагностическую консоль.

Технические параметры не занимают основной экран и нужны только для специальных сценариев или диагностики.

Поиск Gradle-артефактов запускается лениво при первом открытии расширенного режима. Простой запуск не получает owner map, class graph, ASM diagnostics, DI catalog, фильтры или настройки оформления из скрытой формы; это особенно важно, когда логи принадлежат не открытому проекту.

Команда `jankhunter problems` и её интеграция с request/command pipeline сохранены как задел, но кнопка экспорта Problems CSV пока скрыта из UI. Экспорт создаёт плоскую таблицу найденных проблем кода и не заменяет основной HTML-отчёт.

## Каталог отчётов

По умолчанию результаты сохраняются вне открытого проекта:

```text
~/JankHunter/reports/<source>/report-YYYY-MM-DD-HH-mm-ss.html
~/JankHunter/reports/<source>/compare-YYYY-MM-DD-HH-mm-ss.html
```

`<source>` строится из имени папки с логами. Каталог можно изменить на простом экране или в `Settings → Jank Hunter`. HTML после успешной генерации по умолчанию открывается системным браузером.

## CLI

Плагин использует локальную утилиту `jankhunter`. Поиск выполняется автоматически среди типичных путей, включая:

```text
<project>/cli/bin/jankhunter
<project>/../cli/bin/jankhunter
~/.jankhunter/bin/jankhunter
/opt/homebrew/bin/jankhunter
/usr/local/bin/jankhunter
PATH
```

Для разработки CLI обычно собирается так:

```bash
cd ../cli
make build
```

Нестандартный путь задаётся в расширенном режиме или `Settings → Jank Hunter`.

## Конфигурация запуска

Тип Run Configuration `Jank Hunter` сохранён для повторяемых и автоматизируемых проверок. Он использует тот же `JankHunterCommandBuilder`, что и плавающее окно.

Контекстное действие `Inspect Jank Hunter Evidence For Class` открывает расширенный режим и подставляет класс около caret в фильтр анализа.

## Требования

- IntelliJ IDEA/Android Studio на IntelliJ Platform с JBR 21;
- встроенный Gradle wrapper из `plugin-as`;
- доступный исполняемый файл `jankhunter`.

Текущие параметры:

```text
pluginVersion=0.1.1
platformVersion=2026.1.3
pluginSinceBuild=253
```

## Сборка и проверка

```bash
./gradlew test
./gradlew buildPlugin
./gradlew verifyPlugin
```

ZIP появляется в `build/distributions/`.

Для запуска sandbox IDE:

```bash
./gradlew runIde
```

Локальную IDE можно переопределить:

```bash
./gradlew runIde -PlocalIdePath="/Applications/Android Studio.app"
```

## Структура

```text
plugin-as/src/main/kotlin/io/jankhunter/plugin/
  actions/     действия IDE
  execution/   модели входов, discovery, команды и валидация
  problems/    Problems parser и source navigation
  run/         Run Configuration
  services/    процессы, ADB, уведомления и project service
  settings/    persistent settings
  ui/          простой и расширенный UI плавающего окна
```
