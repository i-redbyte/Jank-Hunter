# Сравнение с LeakCanary

Sample app подключает LeakCanary только в debug-сборке и отправляет одни и те же retained objects в оба инструмента:

- Jank Hunter пишет `.jhlog` с screen, flow, step, owner, counters и опциональным heap evidence.
- LeakCanary получает объекты через `AppWatcher.objectWatcher.expectWeaklyReachable(...)`, при достижении retained-threshold делает heap dump, анализирует его и показывает результат через notification или launcher report.
- Release-сборка содержит no-op bridge, поэтому классы LeakCanary не попадают в production artifact.

Интеграция соответствует официальной схеме LeakCanary 2.14: `debugImplementation("com.squareup.leakcanary:leakcanary-android:2.14")`. LeakCanary автоматически стартует в debug-сборке, а кастомные объекты отправляются в современный reachability API `AppWatcher.objectWatcher.expectWeaklyReachable(...)`.

Источники:

- [LeakCanary Getting Started](https://square.github.io/leakcanary/getting_started/)
- [How LeakCanary works](https://square.github.io/leakcanary/fundamentals-how-leakcanary-works/)
- [LeakCanary Code Recipes](https://square.github.io/leakcanary/recipes/)
- [LeakCanary Change Log](https://square.github.io/leakcanary/changelog/)

## Как Запустить

1. Запустите sample app через `./run-sample-app.sh`.
2. В приложении откройте блок `Бенчмарк LeakCanary`.
3. Нажмите `Оба: чистый объект` для negative control.
4. Нажмите `Оба: удержанный объект` или `Оба: cache burst` для positive leak cases.
5. Подождите несколько секунд. Если LeakCanary еще не сделал dump, сверните приложение или откройте notification / launcher report.
6. В shell скрипта выполните `log` или `report`, чтобы забрать `.jhlog` и собрать HTML Jank Hunter.

## Что Сравнивать

| Вопрос | LeakCanary | Jank Hunter |
| --- | --- | --- |
| Нашелась ли утечка? | Retained count, heap analysis result и leak signature. | `report-leaks.html`: retained count, class, owner, flow и свежесть evidence. |
| Почему объект удерживается? | Сильная reference path от GC root до leaking object. | Light mode: holder/scope/context. Heap mode: GC root chain, holder field, retained size и alternative paths. |
| Это регрессия? | Обычно ручное сравнение отдельных отчетов. | `compare-leaks.html`: new, worse, same, better и resolved leak signatures. |
| Понятно ли джуну? | Очень точный, но плотный heap trace. | Продуктовая сводка, severity, suspect owner, flow/step и чеклист расследования. |
| Связано ли это с performance? | Фокус только на утечках. | Корреляция с UI-фризами, памятью, сетью, лог-спамом, owners и flows. |

## Оценочная Матрица

Шкала: 0 - у нас ничего не работает, 10 - мы превзошли LeakCanary по этому критерию.

| Критерий | Оценка Jank Hunter | Почему |
| --- | ---: | --- |
| Детект retained objects | 7 | Ручной `watchObject` и sample-сценарии работают, есть light/heap mode. Но автоматическое покрытие Activity/Fragment/ViewModel пока слабее LeakCanary. |
| Heap trace и GC-root точность | 7 | Heap mode дает GC root chain, holder field, retained size и alternative paths, но LeakCanary/Shark зрелее и богаче по reference matchers. |
| Понятность отчета | 9 | Jank Hunter отчет явно проектируется для junior-friendly расследования: summary, severity, owner, flow, checklist. |
| Regression compare | 9 | `compare-leaks.html` сразу показывает new/worse/same/better/resolved; у LeakCanary это в основном ручная работа. |
| Контекст продукта | 9 | Owner/flow/step и связь с UI/network/memory/log spam дают больше продуктового контекста, чем leak-only отчет. |
| Runtime/feature flag управление | 9 | SDK можно включать/выключать динамически без перезапуска приложения; это сильная business-фича. |
| Production/CI artifact story | 8 | `.jhlog` и HTML хорошо ложатся в QA/CI artifacts; live LeakCanary удобнее локально в debug. |
| Зрелость и доверие | 6 | LeakCanary - отраслевой стандарт, его алгоритмы и UX проверены годами; Jank Hunter еще надо прогнать на большом наборе реальных приложений. |

Средняя оценка текущей реализации: **8.0 / 10**.

Текущий вывод: Jank Hunter уже сильнее LeakCanary как продуктовый отчет, regression analyzer и performance-context инструмент. LeakCanary пока сильнее как зрелый автоматический heap leak detector с эталонной точностью анализа reference path.
