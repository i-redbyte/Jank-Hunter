// The overview counts observed thread generations. Operations and coroutine
// records only identify a TID, so they cannot establish a generation or owner.
(function () {
  const helpers = typeof module !== 'undefined' ? require('./thread-snapshot.js') : {
    threadObservedDuration, threadAnalysisData, threadInScope, threadChartCounts, sourceRoots,
    threadOriginCategory, originLabel, threadSearchText, threadSource, threadNameHint,
    threadCollectorGenerations, threadOwned
  };
  const valid = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
  const key = (scope, tid) => scope + '|' + String(tid);
  const returnedAfterLastObservation = (event, observations) => {
    const last = observations.at(-1)?.ElapsedMS;
    if (!event?.observed_elapsed_nanos || !Number.isSafeInteger(last)) return false;
    try { return BigInt(event.observed_elapsed_nanos) > BigInt(last) * 1000000n; }
    catch { return false; }
  };
  const add = (map, scope, tid, kind, source) => {
    if (!tid) return;
    const id = key(scope, tid);
    let item = map.get(id);
    if (!item) { item = {count: 0, kinds: new Map(), examples: []}; map.set(id, item); }
    item.count++;
    if (kind) item.kinds.set(kind, (item.kinds.get(kind) || 0) + 1);
    if (source && item.examples.length < 3 && !item.examples.includes(source)) item.examples.push(source);
  };
  function threadOverviewIndex(data) {
    const resources = new Map(), coroutines = new Map(), generations = new Map(), javaGenerations = new Map(),
      starts = new Map(), factories = new Map();
    (data.Resources || []).forEach(item => add(resources, item.Scope, item.CompletionTID, item.Kind, item.Source));
    (data.CoroutineSegments || []).forEach(item => {
      const tids = new Set([item.EntryTID, item.ExitTID].filter(Boolean));
      tids.forEach(tid => add(coroutines, item.Scope, tid, '', item.Owner));
    });
    (data.Processes || []).forEach(process => (process.Histories || []).forEach(history => {
      const id = key(process.Scope, history.TID);
      generations.set(id, (generations.get(id) || 0) + 1);
      const javaIDs = new Set((history.Observations || []).map(point => point.Row?.java?.id).filter(Boolean));
      javaIDs.forEach(javaID => {
        const javaKey = key(process.Scope, javaID);
        javaGenerations.set(javaKey, (javaGenerations.get(javaKey) || 0) + 1);
      });
    }));
    (data.Lifecycle || []).forEach(item => {
      const event = item.Observation;
      if ((event?.stage !== 2 && event?.stage !== 3) || !event.java_id ||
          !(event.source || (event.stage === 3 && event.factory))) return;
      const id = key(item.Scope, event.java_id);
      const target = event.stage === 2 ? starts : factories;
      const matches = target.get(id) || [];
      if (matches.length < 2) matches.push(event);
      target.set(id, matches);
    });
    return {resources, coroutines, generations, javaGenerations, starts, factories};
  }
  function threadOverviewView(process, index, filters) {
    const roots = helpers.sourceRoots(filters.roots);
    const query = (filters.search || '').trim().toLowerCase();
    const collectorGenerations = helpers.threadCollectorGenerations(process);
    const matches = point => helpers.threadInScope(point.Row, filters.excludeOwned, filters.source || '', filters.origin || '', roots, collectorGenerations) &&
      (!query || helpers.threadSearchText(point.Row).includes(query));
    const rows = [];
    for (const history of process.Histories || []) {
      const observations = history.Observations || [];
      let row = null, sourceRow = null, source = '', cpuMS = 0, cpuSamples = 0;
      let collectorConfirmed = false, collectorJavaSource = false, priorSelected = false;
      const javaIDs = new Set();
      for (const point of observations) {
        const current = point.Row || {};
        if (current.java?.id) javaIDs.add(current.java.id);
        if (helpers.threadOwned(current)) collectorConfirmed = true;
        if (helpers.threadSource(current).startsWith('Jank Hunter ')) collectorJavaSource = true;
        const selected = matches(point);
        if (selected) {
          row = current;
          const observed = helpers.threadSource(current);
          if (!source && observed) { source = observed; sourceRow = current; }
          const cpu = point.CPU?.CPUTimeMS;
          if (priorSelected && valid(cpu)) { cpuMS += cpu; cpuSamples++; }
        }
        priorSelected = selected;
      }
      if (!row) continue;
      const id = key(process.Scope, history.TID);
      const tidReused = (index.generations.get(id) || 0) > 1;
      const javaIdentityAmbiguous = javaIDs.size > 1 ||
        (javaIDs.size === 1 && (index.javaGenerations.get(key(process.Scope, javaIDs.values().next().value)) || 0) > 1);
      const linkedJavaID = javaIDs.size === 1 && !javaIdentityAmbiguous ? javaIDs.values().next().value : null;
      const startEvents = linkedJavaID ? index.starts.get(key(process.Scope, linkedJavaID)) || [] : [];
      const factoryEvents = linkedJavaID ? index.factories.get(key(process.Scope, linkedJavaID)) || [] : [];
      // A new thread can run and finish before Thread.start or a custom factory
      // returns. The scoped Java identity is the join; event ordering is not.
      const start = startEvents.length === 1 ? startEvents[0] : null;
      const factory = factoryEvents.length === 1 ? factoryEvents[0] : null;
      rows.push({history, row, source, cpuMS, cpuSamples,
        origin: helpers.threadOriginCategory(sourceRow || row, roots, collectorGenerations),
        collectorConfirmed, collectorJavaSource,
        resources: tidReused ? null : index.resources.get(id),
        coroutines: tidReused ? null : index.coroutines.get(id),
        tidReused,
        javaIdentityAmbiguous,
        startSource: start?.source || '', startAmbiguous: startEvents.length > 1,
        factorySource: factory?.source || '', factoryName: factory?.factory || '',
        factoryAmbiguous: factoryEvents.length > 1,
        startReturnedAfterLast: returnedAfterLastObservation(start, observations),
        factoryReturnedAfterLast: returnedAfterLastObservation(factory, observations),
        nameHint: helpers.threadNameHint(row)});
    }
    let peak = 0;
    for (const snapshot of process.Snapshots || []) {
      let count = 0;
      for (const row of snapshot.Snapshot?.threads || []) {
        if (helpers.threadInScope(row, filters.excludeOwned, filters.source || '', filters.origin || '', roots, collectorGenerations) &&
            (!query || helpers.threadSearchText(row).includes(query))) count++;
      }
      peak = Math.max(peak, count);
    }
    const resourceTIDs = new Set(rows.filter(item => item.resources).map(item => String(item.history.TID)));
    const operationCount = [...resourceTIDs].reduce((sum, tid) => sum + index.resources.get(key(process.Scope, tid)).count, 0);
    return {rows, peak, cpuMS: rows.reduce((sum, item) => sum + item.cpuMS, 0),
      cpuSamples: rows.reduce((sum, item) => sum + item.cpuSamples, 0),
      disappeared: rows.filter(item => item.history.DisappearedByMS > 0).length,
      operationCount, operationTIDs: resourceTIDs.size,
      coroutineTIDs: new Set(rows.filter(item => item.coroutines).map(item => String(item.history.TID))).size,
      unknownOrigins: rows.filter(item => item.origin === 'unknown').length,
      startKnown: rows.filter(item => item.startSource).length,
      factoryKnown: rows.filter(item => item.factorySource || item.factoryName).length,
      executionSourceKnown: rows.filter(item => item.source).length,
      nameHintKnown: rows.filter(item => item.nameHint).length,
      noEvidenceCount: rows.filter(item => !item.startSource && !item.factorySource && !item.factoryName &&
        !item.source && !item.resources && !item.coroutines && !item.nameHint).length};
  }
  if (typeof module !== 'undefined') module.exports = {threadOverviewIndex, threadOverviewView};
  if (typeof document === 'undefined') return;
  document.querySelectorAll('[data-thread-analysis]').forEach(root => {
    const data = helpers.threadAnalysisData(root);
    const index = threadOverviewIndex(data);
    const get = name => root.querySelector('[data-thread-' + name + ']');
    const process = get('process'), origin = get('origin'), roots = get('app-roots');
    const source = get('source'), exclude = get('exclude-owned'), search = get('overview-search');
    const sort = get('overview-sort'), grid = get('overview-metrics'), list = get('overview-list');
    const status = get('overview-status'), provenance = get('provenance-coverage'),
      prev = get('overview-prev'), next = get('overview-next');
    if (!process || !grid || !list) return;
    let page = 0;
    const metric = (label, value, note) => {
      const card = document.createElement('div'); card.className = 'thread-kpi';
      const name = document.createElement('span'); name.textContent = label;
      const number = document.createElement('strong'); number.textContent = value;
      const description = document.createElement('small'); description.textContent = note;
      card.append(name, number, description); grid.append(card);
    };
    const ms = value => (value / 1000).toLocaleString('ru-RU', {maximumFractionDigits: 2}) + ' с';
    const calendar = value => valid(value) && value > 0 ? new Date(value).toLocaleString('ru-RU') : 'неизвестно';
    const relative = (value, start) => valid(value) && valid(start) && value >= start ? ms(value - start) + ' от первого снимка' : 'время неизвестно';
    const line = (card, label, value) => {
      const p = document.createElement('p');
      const b = document.createElement('b'); b.textContent = label + ': ';
      p.append(b, document.createTextNode(value)); card.append(p);
    };
    function render() {
      const p = (data.Processes || [])[Number(process.value) || 0];
      grid.replaceChildren(); list.replaceChildren();
      if (!p) { status.textContent = 'Снимков потоков нет.'; return; }
      const view = threadOverviewView(p, index, {origin: origin.value, roots: roots.value,
        source: source.value.trim(), excludeOwned: exclude.checked, search: search.value});
      metric('Потоков встречено', String(view.rows.length), 'разные поколения TID в снимках');
      metric('Одновременно максимум', String(view.peak), 'в одном прочитанном снимке');
      metric('CPU измерено', view.cpuSamples ? ms(view.cpuMS) : 'нет данных', view.cpuSamples + ' допустимых интервалов; сумма по выбранным потокам');
      metric('Исчезли из снимков', String(view.disappeared), 'конец между двумя наблюдениями');
      metric('Операций с этим TID', String(view.operationCount), 'на ' + view.operationTIDs + ' TID; переиспользованные TID исключены');
      metric('Потоков с корутинами', String(view.coroutineTIDs), 'совпадение TID сегментов');
      if (provenance) provenance.textContent = 'Сведения по выбранным потокам: Thread.start - ' +
        view.startKnown + '; возврат ThreadFactory - ' + view.factoryKnown +
        '; первый run/регистрация - ' + view.executionSourceKnown +
        '; подсказка имени - ' + view.nameHintKnown + ' из ' + view.rows.length +
        '. Без этих сведений - ' + view.noEvidenceCount + '. Числа могут пересекаться; место создания Thread ими не устанавливается.';
      const rows = view.rows.slice();
      if (sort.value === 'cpu') rows.sort((a, b) => b.cpuMS - a.cpuMS || Number(a.history.TID) - Number(b.history.TID));
      else if (sort.value === 'life') rows.sort((a, b) => (helpers.threadObservedDuration(b.history) ?? -1) - (helpers.threadObservedDuration(a.history) ?? -1));
      else rows.sort((a, b) => String(a.row.name || '').localeCompare(String(b.row.name || ''), 'ru'));
      const pages = Math.max(1, Math.ceil(rows.length / 24)); page = Math.min(page, pages - 1);
      rows.slice(page * 24, (page + 1) * 24).forEach(item => {
        const h = item.history, card = document.createElement('article'); card.className = 'thread-overview-card';
        const title = document.createElement('h4'); title.textContent = (item.row.name || 'Имя неизвестно') + ' · TID ' + h.TID; card.append(title);
        const launch = item.startSource ? 'Thread.start вызван из ' + item.startSource :
          item.factorySource || item.factoryName ? 'ThreadFactory вернула Thread: ' + (item.factoryName || item.factorySource) :
          item.javaIdentityAmbiguous ? 'Java ID неоднозначен; событие запуска нельзя приписать этому поколению' :
          item.startAmbiguous || item.factoryAmbiguous ? 'несколько событий запуска или фабрики; связь неоднозначна' :
          'событие запуска не записано; создатель не установлен';
        line(card, 'Путь запуска', launch);
        const activity = item.source ? 'Первый записанный run: ' + item.source :
          item.coroutines?.examples.length ? 'Сегмент корутины на TID: ' + item.coroutines.examples[0] :
          item.resources?.examples.length ? 'Завершение операции на TID: ' + item.resources.examples[0] :
          'код выполнения на TID не записан';
        line(card, 'Наблюдавшийся код', activity);
        if (item.nameHint) line(card, 'Подсказка по имени', item.nameHint + '; создатель не подтверждён');
        line(card, 'Измеренный CPU', item.cpuSamples ? ms(item.cpuMS) + ' по ' + item.cpuSamples + ' интервалам' : 'нет измеренной дельты');
        line(card, 'Время наблюдения', (helpers.threadObservedDuration(h) == null ? 'неизвестно: порядок часов не подтверждён' : 'не менее ' + ms(helpers.threadObservedDuration(h))) +
          (h.DisappearedByMS > 0 ? '; затем исчез из снимков' : '; конец не наблюдался'));
        const detail = document.createElement('details'); detail.className = 'thread-overview-evidence';
        const summary = document.createElement('summary'); summary.textContent = 'Запуск, операции и точность данных'; detail.append(summary);
        line(detail, 'Старт Linux-задачи', h.KernelStart ? calendar(h.KernelStart.FromUnixMS) + ' - ' + calendar(h.KernelStart.ByUnixMS) + ' (интервал системного тика; не создание Java Thread)' : 'неизвестен');
        line(detail, 'Впервые и в последний раз виден', relative(h.FirstMS, p.StartAtMS) + ' - ' + relative(h.LastMS, p.StartAtMS));
        line(detail, 'Завершение', h.DisappearedByMS > 0 ? 'исчез между ' + relative(h.DisappearedAfterMS, p.StartAtMS) + ' и ' + relative(h.DisappearedByMS, p.StartAtMS) : 'не наблюдалось; точное время неизвестно');
        line(detail, 'Кто запустил', item.startSource ? 'Thread.start() вызван из ' + item.startSource +
          (item.startReturnedAfterLast ? '; возврат записан после последнего снимка' : '') +
          '; место создания объекта неизвестно' : item.javaIdentityAmbiguous ? 'Java ID неоднозначен между поколениями; вызов не приписан' : item.startAmbiguous ? 'несколько вызовов с этим Java ID; источник неоднозначен' : 'вызов Thread.start() для этого потока не подтверждён');
        line(detail, 'Фабрика', item.factorySource || item.factoryName ? 'ThreadFactory.newThread вернула Java Thread: ' + (item.factoryName || 'класс фабрики неизвестен') + (item.factorySource ? '; вызов из ' + item.factorySource : '') + (item.factoryReturnedAfterLast ? '; возврат записан после последнего снимка' : '') + '; создание и запуск отдельно не подтверждены' : item.javaIdentityAmbiguous ? 'Java ID неоднозначен между поколениями; возврат не приписан' : item.factoryAmbiguous ? 'несколько возвратов фабрики с этим Java ID; источник неоднозначен' : 'возврат фабрики не записан');
        line(detail, 'Первый записанный run/регистрация', item.source || 'не записан; это не позволяет установить выполнявшийся код');
        const kinds = item.resources ? [...item.resources.kinds].map(([name, count]) => name + ' ' + count).join(', ') : '';
        line(detail, 'Операции', item.tidReused ? 'TID использован повторно; записи нельзя отнести к этому поколению' :
          item.resources ? item.resources.count + ' записей завершения на TID' + (kinds ? ' (' + kinds + ')' : '') : 'записей на TID нет');
        if (item.resources?.examples.length) line(detail, 'Источники операций на TID', item.resources.examples.join(', ') + '; не доказывают владельца потока');
        line(detail, 'Корутины', item.tidReused ? 'TID использован повторно; сегменты нельзя отнести к этому поколению' :
          item.coroutines ? item.coroutines.count + ' записей сегментов на TID' : 'сегментов на TID нет');
        if (item.coroutines?.examples.length) line(detail, 'Источники корутин на TID', item.coroutines.examples.join(', ') + '; не доказывают владельца потока');
        if (item.tidReused) line(detail, 'Точность связи', 'Операции и корутины остаются в исходных разделах без приписывания поколению');
        card.append(detail);
        list.append(card);
      });
      status.textContent = origin.value === 'collector' && exclude.checked ?
        'Чтобы посмотреть внутренние потоки, снимите галочку «Без Jank Hunter».' :
        'Показано ' + (rows.length ? page * 24 + 1 : 0) + '-' + Math.min(rows.length, (page + 1) * 24) + ' из ' + rows.length + '; источник кода не классифицирован у ' + view.unknownOrigins + '.';
      prev.disabled = page === 0; next.disabled = page + 1 >= pages;
    }
    [process, origin, roots, source, exclude, search, sort].forEach(input => {
      input.addEventListener('input', () => { page = 0; render(); });
      input.addEventListener('change', () => { page = 0; render(); });
    });
    prev.addEventListener('click', () => { page--; render(); });
    next.addEventListener('click', () => { page++; render(); });
    render();
  });
})();
