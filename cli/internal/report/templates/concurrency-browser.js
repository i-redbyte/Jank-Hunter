(function () {
  const kindNames = ['Неизвестно', 'Поток', 'Задача Executor', 'Handler post', 'Корутина', 'Попытка Worker', 'Ресурс', 'Операция', 'Периодическая серия', 'Executor', 'Наблюдавшееся исполнение'];
  const operationNames = ['', 'Чтение файла', 'Запись файла', 'Синхронизация файла', 'База данных', 'HTTP', 'Ожидание lock', 'Ожидание monitor', 'WakeLock', 'Чтение ContentResolver', 'Запись ContentResolver'];
  const resourceNames = ['', 'AutoCloseable', 'Lock', 'Monitor', 'WakeLock'];
  const phases = {
    1: ['', 'Конструктор вернулся', 'Фабрика вернула', 'Вызван start', 'start вернулся', 'start выбросил исключение', 'Корневой run начался', 'Корневой run завершился', 'Запрошен interrupt', 'interrupt вернулся', 'interrupt выбросил исключение', 'Завершение наблюдалось'],
    2: ['', 'Передана в Executor', 'Вызов передачи вернулся', 'Вызов передачи выбросил исключение', 'Началось выполнение', 'Выполнение завершилось', 'Запрошена отмена', 'Вызов отмены вернулся', 'Терминальный исход наблюдался'],
    3: ['', 'Вызван post', 'post вернулся', 'Начался dispatch', 'dispatch завершился', 'Запрошено удаление', 'Вызов удаления вернулся', 'post выбросил исключение', 'remove выбросил исключение'],
    4: ['', 'Создание наблюдалось', 'Возобновилась', 'Приостановилась', 'Запрошена отмена', 'Исход наблюдался', 'Впервые наблюдалась корутина; создание неизвестно'],
    5: ['', 'Привязана к WorkManager', 'Началась', 'Возвращён retry', 'Запрошена остановка', 'Исход наблюдался', 'Метод Worker наблюдался'],
    6: ['', 'Попытка захвата', 'Захвачен', 'Захват не удался', 'Освобождён', 'Ожидание измерено', 'Передан другому scope', 'Передан в use; приобретение и открытость неизвестны'],
    7: ['', 'Стоимость измерена'],
    8: ['', 'Серия объявлена', 'Завершение наблюдалось', 'Запрошена отмена серии', 'Вызов отмены серии вернулся'],
    9: ['', 'Конструктор вернулся', 'Фабрика вернула', 'Запрошен shutdown', 'shutdown вернулся', 'Завершение наблюдалось', 'Впервые наблюдался', 'shutdown выбросил исключение'],
    10: ['', 'Выполнение началось', 'Выполнение завершилось']
  };
  const kind = value => kindNames[Number(value)] || 'Неизвестно';
  const phase = (entityKind, value) => (phases[Number(entityKind)] || [])[Number(value)] || 'Этап ' + String(value);
  const eventPhase = (entityKind, event) => entityKind === 4 && event.phase === 4 && event.CallResult === 1 ?
    'Вызов отмены вернулся' : entityKind === 4 && event.phase === 4 && event.CallResult === 3 ?
      'Вызов отмены выбросил исключение' : phase(entityKind, event.phase);
  const cost = (bits, bit, value, unit) => Number(bits) & bit ? (unit === 'мс' ? (Number(value ?? 0) / 1e6).toFixed(3) : String(value ?? 0)) + ' ' + unit : 'не измерено';
  const ms = ns => (Number(ns || 0) / 1e6).toFixed(3) + ' мс';
  const relationKey = (scope, kindValue, id) => scope + '/' + kindValue + '/' + id;
  const workerBindingLabel = event => 'Worker instance ' + event.WorkerInstanceID + ' · попытка ' + (event.RunAttempt ?? 0) + ' · поколение ' + (event.Generation ?? 0);
  const coroutineBindingLabel = event => 'Корутина token ' + event.CoroutineToken + ' · поколение ' + (event.CoroutineGeneration ?? 0);
  const nanos = value => BigInt(value || 0);
  const relativeTime = value => {
    const ns = nanos(value);
    if (ns >= 1000000000n) return String(ns / 1000000000n) + '.' + String(ns % 1000000000n / 1000000n).padStart(3, '0') + ' с';
    if (ns >= 1000000n) return String(ns / 1000000n) + '.' + String(ns % 1000000n / 1000n).padStart(3, '0') + ' мс';
    if (ns >= 1000n) return String(ns / 1000n) + ' мкс';
    return String(ns) + ' нс';
  };
  const segmentBounds = {
    1: {start: 6, ends: [7]}, 2: {start: 4, ends: [5]}, 3: {start: 3, ends: [4]},
    4: {start: 2, ends: [3, 5]}, 5: {start: 2, ends: [3, 5]},
    6: {start: 2, ends: [4]}, 10: {start: 1, ends: [2]}
  };

  function causalTimeline(entity) {
    const events = entity.Events || [];
    const first = events.reduce((earliest, event) => {
      const at = nanos(event.observed_ns);
      return earliest === null || at < earliest ? at : earliest;
    }, null);
    const points = events.map((event, index) => ({event, index,
      offset: event.relative_ns === undefined ? nanos(event.observed_ns) - (first || 0n) : nanos(event.relative_ns)}))
      .sort((left, right) => left.offset < right.offset ? -1 : left.offset > right.offset ? 1 : left.index - right.index);
    const declaredSpan = nanos(entity.ObservedSpanNS);
    const span = declaredSpan > 0n ? declaredSpan : (points.length ? points[points.length - 1].offset : 0n);
    const position = offset => span > 0n ? Math.max(0, Math.min(100, Number(offset) / Number(span) * 100)) : 0;
    const boundary = segmentBounds[Number(entity.Kind)];
    const segments = [];
    let started = null;
    if (boundary) for (const point of points) {
      if (point.event.phase === boundary.start || Number(entity.Kind) === 6 && point.event.phase === 7) started = point;
      else if (boundary.ends.includes(point.event.phase) && started) {
        const fromTID = Number(started.event.target_tid || 0);
        const toTID = Number(point.event.target_tid || 0);
        if (point.offset >= started.offset && (!fromTID || !toTID || fromTID === toTID)) {
          segments.push({start: started, end: point, duration: point.offset - started.offset,
            tid: fromTID && toTID && fromTID === toTID ? fromTID : 0});
        }
        started = null;
      }
    }
    return {points, segments, span, position};
  }

  function causalFilter(entities, query, scope) {
    const text = String(query || '').trim().toLocaleLowerCase();
    return (entities || []).filter(entity => {
      if (scope === 'app' && (entity.IsJankHunter || entity.Responsibility === 'unknown')) return false;
      if (scope === 'unknown' && (entity.IsJankHunter || entity.Responsibility !== 'unknown')) return false;
      if (scope === 'jh' && !entity.IsJankHunter) return false;
      if (scope === 'all' && entity.IsJankHunter) return false;
      if (!text) return true;
      return [entity.Source, entity.AppCaller, entity.Process, entity.Scope, entity.Kind, entity.ID, entity.DirectOrigin]
        .some(value => String(value || '').toLocaleLowerCase().includes(text));
    });
  }

  function causalPage(entities, page, size) {
    const total = Math.ceil(entities.length / size);
    const index = Math.max(0, Math.min(Number(page) || 0, Math.max(0, total - 1)));
    return {page: index, total, rows: entities.slice(index * size, index * size + size)};
  }

  if (typeof module !== 'undefined') module.exports = {causalFilter, causalPage, kind, phase, eventPhase, cost, causalTimeline, relativeTime, relationKey, workerBindingLabel, coroutineBindingLabel};
  if (typeof document === 'undefined') return;
  const panel = document.querySelector('.concurrency-panel');
  const details = panel?.querySelector('[data-causal-browser]');
  const script = panel?.querySelector('[data-causal-json]');
  if (!panel || !details || !script) return;
  const search = details.querySelector('[data-causal-search]');
  const scope = details.querySelector('[data-causal-scope]');
  const status = details.querySelector('[data-causal-status]');
  const list = details.querySelector('[data-causal-list]');
  const previous = details.querySelector('[data-causal-previous]');
  const next = details.querySelector('[data-causal-next]');
  const fallback = details.querySelector('[data-causal-fallback]');
  const pageSize = 20;
  let all = null, filtered = [], page = 0, timer = 0, byEntity = new Map();

  const add = (parent, tag, text, className) => {
    const element = document.createElement(tag);
    element.textContent = text;
    if (className) element.className = className;
    parent.appendChild(element);
    return element;
  };
  const codeSite = (source, line) => source ? source + (line ? ':' + line : ' · строка неизвестна') : 'не записан';
  const relation = (parent, label, scopeValue, kindValue, id) => {
    if (!id) return;
    const row = document.createElement('p');
    row.append(document.createTextNode(label + ': '));
    const target = byEntity.get(relationKey(scopeValue, kindValue, id));
    if (target) {
      const link = document.createElement('a');
      link.href = '#' + target.Anchor;
      link.textContent = kind(kindValue) + ' ID ' + id;
      row.append(link);
    } else {
      row.append(document.createTextNode(kind(kindValue) + ' ID ' + id + ' (события не сохранены)'));
    }
    parent.appendChild(row);
  };

  function renderEntity(entity) {
    const article = document.createElement('article');
    article.className = 'concurrency-entity';
    article.id = entity.Anchor;
    add(article, 'h3', kind(entity.Kind) + ' · ID ' + entity.ID);
    add(article, 'p', entity.Process + ' · ' + entity.Scope);
    add(article, 'p', 'Наблюдался не менее ' + ms(entity.ObservedSpanNS) + ' · ' + entity.LifecycleNote);
    add(article, 'p', 'Код этапа: ' + codeSite(entity.Source, entity.SourceLine) + ' · ' + entity.DirectOrigin);
    if (entity.AppCaller) add(article, 'p', 'Связь с приложением: ' + codeSite(entity.AppCaller, entity.AppCallerLine) + ' · ' + entity.AppCallerEvidence);
    if (entity.Kind === 5) add(article, 'p', entity.WorkerAttemptLinked ?
      'Точная связь с WorkManager: ' + (entity.WorkerUUID || 'UUID неизвестен') + ' · исход ' +
      (entity.WorkerOutcome || 'неизвестен') + ' · охват ' + (entity.WorkerCoverage || 'неизвестен') :
      'Точная связь с legacy-попыткой WorkManager не подтверждена');
    if (entity.ParentID) relation(article, 'Родитель', entity.Scope, entity.ParentKind, entity.ParentID);
    else add(article, 'p', 'Родитель: неизвестен');
    if (entity.SchedulerID) relation(article, 'Планировщик', entity.Scope, entity.SchedulerKind, entity.SchedulerID);
    else add(article, 'p', 'Планировщик: неизвестен');
    if (entity.ActorID) relation(article, 'Исполнитель первого этапа', entity.Scope, entity.ActorKind, entity.ActorID);
    const stages = document.createElement('details');
    add(stages, 'summary', 'Шкала, этапы и затраты (' + (entity.Events || []).length + (entity.OmittedEvents ? '; ещё ' + entity.OmittedEvents + ' не показано' : '') + ')');
    const timeline = causalTimeline(entity);
    if (timeline.points.length) {
      add(stages, 'p', 'Время от первого наблюдения этой сущности. Точки — записанные события; полосы — только пары наблюдаемых границ. Промежуток не доказывает непрерывную работу CPU, неизвестные этапы не достраиваются.', 'concurrency-timeline-note');
      const track = document.createElement('div');
      track.className = 'concurrency-timeline-track';
      track.setAttribute('role', 'img');
      track.setAttribute('aria-label', 'Шкала от 0 до ' + relativeTime(timeline.span) + ': ' + timeline.points.length + ' событий, ' + timeline.segments.length + ' подтверждённых пар границ');
      for (const segment of timeline.segments) {
        const bar = document.createElement('span');
        bar.className = 'concurrency-timeline-segment';
        bar.style.left = timeline.position(segment.start.offset) + '%';
        bar.style.width = Math.max(0, timeline.position(segment.end.offset) - timeline.position(segment.start.offset)) + '%';
        bar.title = eventPhase(entity.Kind, segment.start.event) + ' → ' + eventPhase(entity.Kind, segment.end.event) +
          ': +' + relativeTime(segment.start.offset) + ' – +' + relativeTime(segment.end.offset) +
          ' · ' + (segment.tid ? 'TID ' + segment.tid : 'TID неизвестен');
        track.appendChild(bar);
      }
      for (const point of timeline.points) {
        const marker = document.createElement('span');
        marker.className = 'concurrency-timeline-point';
        marker.style.left = timeline.position(point.offset) + '%';
        marker.title = '+' + relativeTime(point.offset) + ' · ' + eventPhase(entity.Kind, point.event);
        track.appendChild(marker);
      }
      stages.appendChild(track);
      if (timeline.segments.length) add(stages, 'p', timeline.segments.map(segment =>
        eventPhase(entity.Kind, segment.start.event) + ' → ' + eventPhase(entity.Kind, segment.end.event) +
        ': +' + relativeTime(segment.start.offset) + '–+' + relativeTime(segment.end.offset) +
        ' (' + relativeTime(segment.duration) + ', ' + (segment.tid ? 'TID ' + segment.tid : 'TID неизвестен') + ')').join(' · '), 'concurrency-timeline-segments');
      if (entity.OmittedEvents) add(stages, 'p', 'Шкала неполна: ' + entity.OmittedEvents + ' событий не показано; их интервалы не восстановлены.', 'concurrency-timeline-note');
    }
    const sequence = document.createElement('ol');
    sequence.className = 'concurrency-event-list';
    for (const point of timeline.points) {
      const event = point.event;
      const line = document.createElement('li');
      add(line, 'strong', eventPhase(entity.Kind, event));
      add(line, 'p', '+' + relativeTime(point.offset) + ' от первого события · monotonic t=' + event.observed_ns + ' нс' + (event.target_tid ? ' · TID ' + event.target_tid : '') +
        (event.TargetJavaID ? ' · Java ID ' + event.TargetJavaID : ''));
      if (event.Source) add(line, 'p', (entity.Kind === 1 && event.phase === 6 ? 'Site создания/target: ' : 'Код этапа: ') +
        codeSite(event.Source, event.SourceLine) + ' · ' + event.SourceOrigin);
      if (entity.Kind === 1 && event.phase === 6) add(line, 'p', 'У wrapped target этот site не доказывает метод Runnable.run.');
      if (event.AppCaller) add(line, 'p', 'Вызов приложения: ' + codeSite(event.AppCaller, event.AppCallerLine));
      if (event.ActorID) relation(line, 'Действие выполнил', entity.Scope, event.ActorKind, event.ActorID);
      if (event.ParentID) relation(line, 'Родитель этапа', entity.Scope, event.ParentKind, event.ParentID);
      if (event.SchedulerID) relation(line, 'Планировщик этапа', entity.Scope, event.SchedulerKind, event.SchedulerID);
      if (event.ResourceKind) add(line, 'p', 'Ресурс: ' + (resourceNames[event.ResourceKind] || 'вид ' + event.ResourceKind) + (event.LeaseScopeID ? ' · scope ' + kind(event.LeaseScopeKind) + ' ID ' + event.LeaseScopeID + (event.BoundToScope ? ' · закрыть до конца scope' : ' · может быть передан') : ''));
      if (entity.Kind === 6 && event.phase === 6 && Number(event.known_fields & 262144) !== 0 && !event.LeaseScopeID) {
        add(line, 'p', 'Lease явно передан за пределы известного scope; новый владелец неизвестен');
      }
      if (event.OperationKind) add(line, 'p', 'Операция: ' + (operationNames[event.OperationKind] || 'вид ' + event.OperationKind) + (event.LeaseID ? ' · lease ID ' + event.LeaseID : ''));
      if (event.WorkerInstanceID) add(line, 'p', workerBindingLabel(event));
      if (event.CoroutineToken) add(line, 'p', coroutineBindingLabel(event));
      if (event.Occurrence) add(line, 'p', 'Исполнение серии №' + event.Occurrence);
      if (entity.Kind === 3 && event.phase === 1 && (Number(event.known_fields) & 2097152)) {
        add(line, 'p', 'Запрошенная задержка Handler: ' + cost(event.known_fields, 2097152, event.RequestedDelayNS, 'мс') + '; это не измеренное ожидание очереди');
      }
      if (event.QueueEnteredNS || event.ScheduledForNS) add(line, 'p', 'Вход очереди t=' + (event.QueueEnteredNS || 'не измерено') + ' · плановый t=' + (event.ScheduledForNS || 'не указан'));
      if (event.CallResult) add(line, 'p', 'Исход вызова: ' + ({1:'вернулся нормально (для boolean-вызова true)',2:'false',3:'исключение'}[event.CallResult] || 'неизвестен'));
      if (event.TerminalOutcome) add(line, 'p', 'Терминальный итог: ' + ({1:'успех',2:'ошибка',3:'отмена',4:'остановка',5:'отклонено'}[event.TerminalOutcome] || 'неизвестен'));
      add(line, 'p', 'Длительность: ' + cost(event.known_fields, 32, event.duration_ns, 'мс') +
        ' · CPU: ' + cost(event.known_fields, 16, event.cpu_ns, 'мс') +
        ' · прочитано: ' + cost(event.known_fields, 64, event.read_bytes, 'байт') +
        ' · записано: ' + cost(event.known_fields, 128, event.write_bytes, 'байт'));
      sequence.appendChild(line);
    }
    stages.appendChild(sequence);
    article.appendChild(stages);
    return article;
  }

  function load() {
    if (all) return true;
    try {
      all = JSON.parse(script.textContent || '[]');
      if (!Array.isArray(all)) throw new Error('bad causal index');
      byEntity = new Map(all.map(item => [relationKey(item.Scope, item.Kind, item.ID), item]));
      fallback.hidden = true;
      return true;
    } catch (_) {
      status.textContent = 'Не удалось открыть индекс причинных событий; первые карточки ниже остаются доступными.';
      return false;
    }
  }

  function render() {
    if (!load()) return;
    filtered = causalFilter(all, search.value, scope.value);
    const current = causalPage(filtered, page, pageSize);
    page = current.page;
    const fragment = document.createDocumentFragment();
    current.rows.forEach(entity => fragment.appendChild(renderEntity(entity)));
    list.replaceChildren(fragment);
    status.textContent = 'Показано ' + current.rows.length + ' из ' + filtered.length + ' подходящих сущностей; страница ' +
      (current.total ? current.page + 1 : 0) + ' из ' + current.total + '. Всего сохранено ' + all.length + '.';
    previous.disabled = page <= 0;
    next.disabled = page + 1 >= current.total;
  }

  details.addEventListener('toggle', () => { if (details.open) render(); });
  search.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(() => { page = 0; render(); }, 75); });
  scope.addEventListener('change', () => { page = 0; render(); });
  previous.addEventListener('click', () => { page--; render(); });
  next.addEventListener('click', () => { page++; render(); });
  panel.addEventListener('click', event => {
    const link = event.target.closest?.('a[href^="#causal-"]');
    if (!link) return;
    const anchor = link.getAttribute('href').slice(1);
    if (!load()) return;
    const target = all.find(item => item.Anchor === anchor);
    if (!target) return;
    event.preventDefault();
    details.open = true;
    search.value = '';
    scope.value = target.IsJankHunter ? 'jh' : target.Responsibility === 'unknown' ? 'unknown' : 'app';
    const visible = causalFilter(all, '', scope.value);
    page = Math.floor(visible.findIndex(item => item.Anchor === anchor) / pageSize);
    render();
    list.querySelector('#' + anchor)?.scrollIntoView({block: 'center'});
  });
})();
