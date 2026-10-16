// Diagnostic candidates are recomputed for the selected population. The
// process-wide signals cannot be displayed unchanged after filtering threads.
(function () {
  const helpers = typeof module !== 'undefined' ? require('./thread-snapshot.js') : {
    threadAnalysisData, threadInScope, threadSearchText, threadSource,
    threadCollectorGenerations, threadOriginCategory, sourceRoots
  };
  const finite = value => typeof value === 'number' && Number.isFinite(value) && value >= 0;
  const elapsed = (from, to) => finite(from) && finite(to) && to >= from ? to - from : null;
  const countRows = (snapshot, matches) => (snapshot?.Snapshot?.threads || []).reduce(
    (count, row) => count + (matches(row) ? 1 : 0), 0);
  const contiguous = (before, after) => {
    const a = before?.Snapshot, b = after?.Snapshot;
    if (!a?.complete || !b?.complete || !finite(a.elapsed_ms) || !finite(b.elapsed_ms) ||
        !finite(a.interval_ms) || a.interval_ms === 0 || a.interval_ms !== b.interval_ms) return false;
    const gap = b.elapsed_ms - a.elapsed_ms;
    return gap > 0 && gap <= 2 * a.interval_ms;
  };
  const location = (history, point) => {
    const row = point?.Row || {};
    const source = helpers.threadSource(row);
    return (row.name || 'Имя неизвестно') + ' · TID ' + history.TID +
      (source ? ' · первый записанный run/регистрация: ' + source : ' · код выполнения не записан');
  };
  const candidate = (code, title, evidence, action, limitation, extra = {}) =>
    ({code, title, evidence, action, limitation, ...extra});
  const sourcePackageHint = source => {
    const parts = source.split('.');
    return parts.length > 3 ? parts.slice(0, 3).join('.') : source;
  };
  const threadNoun = count => count % 10 === 1 && count % 100 !== 11 ? 'поток' :
    [2, 3, 4].includes(count % 10) && ![12, 13, 14].includes(count % 100) ? 'потока' : 'потоков';
  const generationKey = (scope, row) => `${scope}|${row.tid}|${row.start_ticks}`;
  const newGenerations = (before, after, matches, scope) => {
    const old = new Set((before?.Snapshot?.threads || []).map(row => generationKey(scope, row)));
    const related = [];
    for (const row of after?.Snapshot?.threads || []) {
      if (matches(row) && !old.has(generationKey(scope, row))) {
        related.push({tid: row.tid, startTicks: row.start_ticks});
      }
    }
    return related;
  };
  const snapshotElapsed = point => point.Snapshot?.elapsed_ms;
  const previousComplete = (points, time, clock = point => point.TimeMS) => {
    let low = 0, high = points.length;
    while (low < high) {
      const middle = (low + high) >> 1;
      if (clock(points[middle]) < time) low = middle + 1;
      else high = middle;
    }
    return points[low - 1] || null;
  };
  const nextComplete = (points, time, clock = point => point.TimeMS) => {
    let low = 0, high = points.length;
    while (low < high) {
      const middle = (low + high) >> 1;
      if (clock(points[middle]) <= time) low = middle + 1;
      else high = middle;
    }
    return points[low] || null;
  };

  // The two sorts cover complete snapshots and short observed generations;
  // the remaining work scales with the rows and history points in the report.
  // No row is treated as an allocation or as proof of owning a resource.
  function threadDiagnosticInsights(process, filters = {}, droppedSnapshots = 0) {
    const roots = helpers.sourceRoots(filters.roots);
    const collectorGenerations = process ? helpers.threadCollectorGenerations(process) : new Set();
    const query = String(filters.search || '').trim().toLowerCase();
    const matches = row => !!row && helpers.threadInScope(row, !!filters.excludeOwned,
      filters.source || '', filters.origin || '', roots, collectorGenerations) &&
      (!query || helpers.threadSearchText(row).includes(query));
    const snapshots = process?.Snapshots || [];
    const histories = process?.Histories || [];
    const complete = snapshots.filter(point => point?.Snapshot?.complete && finite(point.TimeMS));
    // The observation/record timestamp may move with the system clock. Only
    // the sampler's monotonic clock can bound short lifetimes.
    const lifetimeBounds = complete.filter(point => finite(snapshotElapsed(point)) &&
      finite(point.Snapshot.scan_duration_ms)).sort((a, b) => snapshotElapsed(a) - snapshotElapsed(b));
    const findings = [];
    let observedGenerations = 0, knownOriginGenerations = 0, sourceObservedGenerations = 0,
      unclassifiedSourceGenerations = 0,
      cpuIntervals = 0, selectedCPUms = 0;
    const unclassifiedSources = new Map();
    let burst = null, growth = null, rising = 0;
    for (let i = 1; i < snapshots.length; i++) {
      const before = snapshots[i - 1], after = snapshots[i];
      if (!contiguous(before, after)) { rising = 0; continue; }
      const previous = countRows(before, matches), current = countRows(after, matches);
      const rise = current - previous;
      const gap = after.Snapshot.elapsed_ms - before.Snapshot.elapsed_ms;
      // A Java source may become known on an existing generation. Do not call
      // that a thread burst or growth just because a filter now includes it.
      const fresh = rise > 0 ? newGenerations(before, after, matches, process?.Scope || '') : [];
      const generationRise = rise > 0 && fresh.length >= rise;
      if (generationRise && rise >= 8 && gap <= 10000 && (!burst || rise > burst.rise)) {
        burst = {rise, previous, current, from: before.TimeMS, to: after.TimeMS, gap, before, after};
      }
      rising = generationRise ? rising + 1 : 0;
      if (rising >= 3) {
        const first = snapshots[i - 3];
        const baseline = countRows(first, matches);
        if (current - baseline >= 5 && (!growth || current - baseline > growth.rise)) {
          growth = {rise: current - baseline, previous: baseline, current,
            from: first.TimeMS, to: after.TimeMS, before: first, after};
        }
      }
    }
    if (growth) {
      findings.push(candidate('observed_growth', 'Число потоков устойчиво росло',
        `В четырёх последовательных полных снимках выбранной области: ${growth.previous} → ${growth.current} (+${growth.rise}).`,
        'Проверьте состав новых поколений и настройки пулов в этом окне.',
        'Это рост числа прочитанных потоков, а не доказательство утечки. Изменение источника или категории также может менять выбранную область.',
        {fromMS: growth.from, toMS: growth.to,
          related: newGenerations(growth.before, growth.after, matches, process?.Scope || '').slice(0, 8)}));
    } else if (burst) {
      findings.push(candidate('observed_burst', 'Резкий прирост числа потоков',
        `Между двумя полными снимками за ${burst.gap} мс число потоков выбранной области выросло с ${burst.previous} до ${burst.current} (+${burst.rise}).`,
        'Проверьте, запускались ли в этом окне новые пулы или задачи.',
        'Это изменение состава снимков, а не число вызовов Thread.start. Намеренный запуск пула допустим; фильтрация по источнику может изменить состав.',
        {fromMS: burst.from, toMS: burst.to,
          related: newGenerations(burst.before, burst.after, matches, process?.Scope || '').slice(0, 8)}));
    }

    const hot = [], waits = [], shortLived = [];
    let hotCount = 0, waitCount = 0;
    for (const history of histories) {
      const points = history.Observations || [];
      if (!points.some(point => matches(point.Row))) continue;
      observedGenerations++;
      let knownOrigin = false, observedSource = '';
      let bestHot = null, waiting = 0, firstWait = null, previousElapsed = -1;
      for (let i = 0; i < points.length; i++) {
        const point = points[i], prior = points[i - 1];
        const selected = matches(point.Row);
        if (selected) {
          if (!knownOrigin && helpers.threadOriginCategory(point.Row, roots, collectorGenerations) !== 'unknown') knownOrigin = true;
          if (!observedSource) observedSource = helpers.threadSource(point.Row);
        }
        const cpu = point.CPU;
        if (selected && prior && matches(prior.Row) && cpu && finite(cpu.CPUTimeMS) &&
            finite(cpu.ElapsedMS) && finite(cpu.CorePercent) && cpu.ElapsedMS > 0) {
          cpuIntervals++;
          selectedCPUms += cpu.CPUTimeMS;
          if (cpu.ElapsedMS >= 1000 && cpu.CorePercent >= 80 &&
              (!bestHot || cpu.CorePercent > bestHot.cpu.CorePercent)) bestHot = {point, cpu};
        }
        if (selected && point.Row.os_state === 'D' && finite(point.ElapsedMS) &&
            point.ElapsedMS > previousElapsed) {
          if (waiting === 0) firstWait = point;
          waiting++;
          if (waiting === 3) {
            waitCount++;
            if (waits.length < 3) waits.push({history, first: firstWait, last: point});
          }
        } else {
          waiting = 0;
          firstWait = null;
        }
        previousElapsed = point.ElapsedMS;
      }
      if (knownOrigin) knownOriginGenerations++;
      if (observedSource) {
        sourceObservedGenerations++;
        if (!knownOrigin) {
          unclassifiedSourceGenerations++;
          const prefix = sourcePackageHint(observedSource);
          unclassifiedSources.set(prefix, (unclassifiedSources.get(prefix) || 0) + 1);
        }
      }
      if (bestHot) {
        hotCount++;
        hot.push({history, ...bestHot});
        hot.sort((a, b) => b.cpu.CorePercent - a.cpu.CorePercent);
        if (hot.length > 3) hot.pop();
      }

      // Full snapshots on both sides bound the observed generation, even if
      // their wall-clock timestamps came from a record-time fallback.
      let firstElapsed = Infinity, lastElapsed = -Infinity, validClock = points.length > 0;
      for (const point of points) {
        if (!finite(point.ElapsedMS)) { validClock = false; break; }
        firstElapsed = Math.min(firstElapsed, point.ElapsedMS);
        lastElapsed = Math.max(lastElapsed, point.ElapsedMS);
      }
      if (!validClock) continue;
      const before = previousComplete(lifetimeBounds, firstElapsed, snapshotElapsed);
      const after = nextComplete(lifetimeBounds, lastElapsed, snapshotElapsed);
      if (!before || !after) continue;
      const upper = elapsed(snapshotElapsed(before), snapshotElapsed(after) + after.Snapshot.scan_duration_ms);
      if (upper !== null && upper <= 15000) shortLived.push({history, upper, firstElapsed});
    }

    for (const item of hot) {
      findings.push(candidate('measured_cpu', 'Измерена высокая загрузка потока',
        `${item.cpu.CorePercent.toFixed(1)}% одного ядра в среднем за ${item.cpu.ElapsedMS} мс; CPU-время ${item.cpu.CPUTimeMS.toFixed(1)} мс.`,
        'Проверьте стек и операции этого потока в указанном интервале; сопоставьте с пользовательским сценарием.',
        'Высокая загрузка сама по себе не ошибка. Краткие пики между снимками и полное CPU-время процесса неизвестны.',
        {tid: item.history.TID, startTicks: item.history.StartTicks,
          related: [{tid: item.history.TID, startTicks: item.history.StartTicks}],
          where: location(item.history, item.point), toMS: item.point.TimeMS}));
    }
    if (hotCount > 3) findings.push(candidate('measured_cpu_more', `Ещё ${hotCount - 3} ${threadNoun(hotCount - 3)} с высокой загрузкой`,
      `У ${hotCount} поколений есть интервал не короче 1 с со средней нагрузкой ≥80% одного ядра.`,
      'Сузьте выбор по имени или происхождению, чтобы увидеть конкретные потоки.',
      'Интервалы разных потоков не обязательно совпадают по времени.'));
    for (const item of waits) {
      findings.push(candidate('repeated_os_wait', 'Повторное непрерываемое ожидание ОС',
        'Одно поколение потока имело состояние ОС D в трёх последовательных наблюдениях.',
        'Проверьте I/O и снимите стек потока в этом окне.',
        'Состояние между наблюдениями неизвестно. D не означает Java BLOCKED и не доказывает deadlock.',
        {tid: item.history.TID, startTicks: item.history.StartTicks,
          related: [{tid: item.history.TID, startTicks: item.history.StartTicks}],
          where: location(item.history, item.last), fromMS: item.first.TimeMS, toMS: item.last.TimeMS}));
    }
    if (waitCount > 3) findings.push(candidate('repeated_os_wait_more', `Ещё ${waitCount - 3} ${threadNoun(waitCount - 3)} с повторным ожиданием`,
      `У ${waitCount} поколений записаны три последовательных состояния D.`,
      'Сузьте выбор по имени или происхождению и проверьте стек в этом окне.',
      'Причина ожидания и состояние между наблюдениями неизвестны.'));

    // Repeated short bounded generations suggest avoidable Thread churn, not
    // confirmed Java Thread creation or confirmed pool ownership.
    shortLived.sort((a, b) => a.firstElapsed - b.firstElapsed);
    let churn = null, left = 0;
    for (let right = 0; right < shortLived.length; right++) {
      while (shortLived[right].firstElapsed - shortLived[left].firstElapsed > 60000) left++;
      const count = right - left + 1;
      if (count >= 8 && (!churn || count > churn.count)) {
        churn = {count, fromElapsed: shortLived[left].firstElapsed, toElapsed: shortLived[right].firstElapsed,
          related: shortLived.slice(left, Math.min(right + 1, left + 8)).map(item =>
            ({tid: item.history.TID, startTicks: item.history.StartTicks}))};
      }
    }
    if (churn) findings.push(candidate('short_generations', 'Частая смена коротких поколений потоков',
      `${churn.count} поколений впервые появились и затем исчезли между полными снимками в окне до 60 с; измеренный коридор каждого - не более 15 с.`,
      'Проверьте, не создаются ли отдельные Thread для каждой задачи; сопоставьте имена и источники с настройками Executor.',
      'Снимки неатомарны: это коридор наблюдений Linux TID, не точное время жизни и не число созданий Java Thread. Имя не доказывает принадлежность пулу.',
      {fromElapsedMS: churn.fromElapsed, toElapsedMS: churn.toElapsed, related: churn.related}));

    const order = {observed_growth: 0, observed_burst: 0, short_generations: 1,
      measured_cpu: 2, repeated_os_wait: 3, measured_cpu_more: 4, repeated_os_wait_more: 4};
    findings.sort((a, b) => (order[a.code] ?? 9) - (order[b.code] ?? 9));
    return {findings, coverage: {snapshots: snapshots.length, completeSnapshots: complete.length,
      partialSnapshots: snapshots.length - complete.length, droppedSnapshots,
      observedGenerations, knownOriginGenerations, sourceObservedGenerations,
      unclassifiedSourceGenerations,
      unclassifiedSourceHints: [...unclassifiedSources].sort((a, b) => b[1] - a[1]).slice(0, 3).map(([prefix]) => prefix),
      cpuIntervals, selectedCPUms}};
  }
  if (typeof module !== 'undefined') module.exports = {threadDiagnosticInsights, previousComplete};
  if (typeof document === 'undefined') return;
  document.querySelectorAll('[data-thread-analysis]').forEach(root => {
    const target = root.querySelector('[data-thread-insights]');
    if (!target) return;
    const data = helpers.threadAnalysisData(root);
    const get = name => root.querySelector('[data-thread-' + name + ']');
    const process = get('process'), origin = get('origin'), roots = get('app-roots'),
      source = get('source'), exclude = get('exclude-owned'), search = get('overview-search');
    if (!process || !origin || !roots || !source || !exclude || !search) return;
    const addText = (parent, tag, value) => {
      const node = document.createElement(tag); node.textContent = value; parent.append(node); return node;
    };
    const relative = (time, start) => {
      const difference = elapsed(start, time);
      return difference === null ? 'время неизвестно' : `${(difference / 1000).toFixed(1)} с после первого снимка`;
    };
    function render() {
      target.replaceChildren();
      const selected = data.Processes?.[Number(process.value) || 0];
      if (!selected) { addText(target, 'p', 'Снимков потоков нет; диагностические выводы недоступны.'); return; }
      const result = threadDiagnosticInsights(selected, {origin: origin.value, roots: roots.value,
        source: source.value.trim(), excludeOwned: exclude.checked, search: search.value}, data.DroppedSnapshots || 0);
      const header = document.createElement('div'); header.className = 'thread-insights-head';
      addText(header, 'h3', 'Что требует проверки');
      addText(header, 'p', result.findings.length ? `${result.findings.length} сигналов для проверки в выбранной области` :
        'Сигналов для проверки в выбранной области пока нет');
      target.append(header);
      const c = result.coverage;
      if (c.observedGenerations && (c.unclassifiedSourceGenerations || c.knownOriginGenerations === 0)) {
        const message = c.unclassifiedSourceGenerations ?
          `У ${c.unclassifiedSourceGenerations} потоков записан источник первого run/регистрации, но его пакет не распознан как пакет приложения, системы или известной библиотеки. ` +
          (c.unclassifiedSourceHints.length ? `Частые префиксы: ${c.unclassifiedSourceHints.join(', ')}. ` : '') +
          'Если это ваш код, добавьте его префиксы в поле «Пакеты приложения».' :
          'Источник первого инструментированного run/регистрации не записан ни для одного выбранного потока. Имена помогают сузить поиск, но не доказывают, кто создал или запустил поток.';
        const originQuality = addText(target, 'p', message);
        originQuality.className = 'thread-insights-origin';
        if (c.unclassifiedSourceHints.length) {
          const suggestions = document.createElement('div');
          suggestions.className = 'thread-origin-suggestions';
          addText(suggestions, 'span', 'Добавьте префикс, только если он относится к коду приложения:');
          c.unclassifiedSourceHints.forEach(prefix => {
            const button = addText(suggestions, 'button', prefix);
            button.type = 'button';
            button.addEventListener('click', () => {
              const selected = helpers.sourceRoots(roots.value);
              if (!selected.includes(prefix)) roots.value = [...selected, prefix].join(', ');
              roots.dispatchEvent(new Event('input', {bubbles: true}));
            });
          });
          target.append(suggestions);
        }
      }
      if (!result.findings.length) {
        const status = addText(target, 'p', 'Отсутствие сигнала не означает отсутствия проблем: короткие потоки между снимками и интервалы без CPU-дельты могли быть пропущены.');
        status.className = 'thread-insights-empty';
      }
      const list = document.createElement('div'); list.className = 'thread-insights-list';
      result.findings.forEach(item => {
        const card = document.createElement('article'); card.className = 'thread-insight-card';
        addText(card, 'h4', item.title);
        addText(card, 'p', item.evidence);
        if (item.where) addText(card, 'p', item.where);
        if (item.fromMS || item.toMS) addText(card, 'small',
          'Окно наблюдения: ' + relative(item.fromMS || item.toMS, selected.StartAtMS) +
          (item.fromMS && item.toMS ? ' - ' + relative(item.toMS, selected.StartAtMS) : ''));
        if (item.fromElapsedMS != null && item.toElapsedMS != null) addText(card, 'small',
          `Окно по монотонным часам: ${item.fromElapsedMS}-${item.toElapsedMS} мс elapsedRealtime.`);
        const next = addText(card, 'p', 'Что проверить: ' + item.action); next.className = 'thread-insight-action';
        const limit = addText(card, 'small', item.limitation); limit.className = 'thread-insight-limit';
        if (item.related?.length) {
          const route = document.createElement('div'); route.className = 'thread-insight-route';
          addText(route, 'span', item.related.length > 1 ? 'Впервые наблюдавшиеся TID в окне: ' : 'Поток: ');
          item.related.forEach((generation, i) => {
            const button = addText(route, 'button', `TID ${generation.tid}`);
            button.type = 'button';
            button.title = `Поколение Linux TID: start_ticks ${generation.startTicks}`;
            button.addEventListener('click', () => {
              search.value = String(generation.tid);
              search.dispatchEvent(new Event('input', {bubbles: true}));
              root.querySelector('[data-thread-overview-list]')?.scrollIntoView({block: 'start'});
            });
            if (i + 1 < item.related.length) route.append(document.createTextNode(' '));
          });
          card.append(route);
        }
        list.append(card);
      });
      target.append(list);
      const quality = addText(target, 'p', `Основание: ${c.completeSnapshots} полных снимков из ${c.snapshots}; ` +
        `${c.observedGenerations} наблюдаемых поколений, ${c.cpuIntervals} измеренных интервалов CPU.` +
        (c.partialSnapshots || c.droppedSnapshots ? ` Неполных снимков ${c.partialSnapshots}, отброшено CLI ${c.droppedSnapshots}.` : ''));
      quality.className = 'thread-insights-quality';
    }
    [process, origin, roots, source, exclude, search].forEach(input => {
      input.addEventListener('input', render);
      input.addEventListener('change', render);
    });
    render();
  });
})();
