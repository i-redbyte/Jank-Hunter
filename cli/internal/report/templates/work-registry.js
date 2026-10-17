(() => {
  const registryLatestSnapshotIndex = snapshots => {
    let latest=-1, time=-1;
    snapshots.forEach((snapshot,index)=>{
      if(snapshot.ObservedUnixMS == null)return;
      const value=snapshot.ObservedUnixMS;
      if(value>=time) {latest=index;time=value;}
    });
    return latest < 0 ? snapshots.length-1 : latest;
  };
  const snapshotPageSize = 200;
  const registrySnapshotRange = (total, requestedPage) => {
    const pages = Math.max(1, Math.ceil(total / snapshotPageSize));
    const page = Math.max(0, Math.min(Math.floor(requestedPage), pages - 1));
    return {start: page * snapshotPageSize, end: Math.min(total, (page + 1) * snapshotPageSize), page, pages};
  };
  const registryJobHistory = (snapshots, selected, uuid) => {
    const retained = new Array(200);
    let total = 0;
    for (const snapshot of snapshots) {
      if (snapshot.Scope !== selected.Scope || snapshot.ObservedElapsedMS > selected.ObservedElapsedMS) continue;
      for (const job of (snapshot.Jobs || [])) {
        if (job.UUID !== uuid) continue;
        retained[total % retained.length] = {snapshot, job};
        total++;
      }
    }
    const observations = [];
    for (let index = Math.max(0, total - retained.length); index < total; index++) observations.push(retained[index % retained.length]);
    return {observations, total};
  };
  const registryProgressTimeline = history => history.observations.map(({snapshot,job})=>({
    ObservedElapsedMS:snapshot.ObservedElapsedMS,Complete:!!snapshot.Complete,
    Progress:job.Progress || [],OmittedProgressKeys:job.OmittedProgressKeys || 0,
  }));
  const registryFindJob = (snapshots, scope, uuid) => {
    let found = -1;
    snapshots.forEach((s,index) => {
      if (s.Scope === scope && (s.Jobs || []).some(j => j.UUID === uuid) &&
          (found < 0 || s.ObservedElapsedMS >= snapshots[found].ObservedElapsedMS)) found = index;
    });
    return found;
  };
  if (typeof module !== 'undefined') module.exports = {registryLatestSnapshotIndex,registrySnapshotRange,
    registryJobHistory, registryProgressTimeline, registryFindJob};
  if (typeof document === 'undefined') return;
  const root = document.querySelector('[data-work-registry]');
  if (!root) return;
  const data = JSON.parse(root.querySelector('script[type="application/json"]').textContent);
  const snapshots = data.Snapshots || [];
  const select = root.querySelector('[data-registry-snapshot]');
  const search = root.querySelector('input');
  const stateFilter = root.querySelector('[data-registry-state]');
  const body = root.querySelector('tbody');
  const info = root.querySelector('[data-registry-info]');
  const pageLabel = root.querySelector('[data-registry-page]');
  const card = root.querySelector('[data-registry-card]');
  const chainRoot = document.querySelector('[data-work-chain]');
  const chainData = chainRoot ? JSON.parse(chainRoot.querySelector('script[type="application/json"]').textContent) : {};
  const chainEdges = chainData.Edges || [], chainRoots = chainData.Roots || [];
  const states = ['ожидает', 'выполняется по наблюдению', 'успешно', 'ошибка', 'заблокировано зависимостями', 'отменено'];
  const chainPolicies = ['обычная цепочка','KEEP','REPLACE','APPEND','APPEND_OR_REPLACE'];
  const reasons = {missing_packets:'потеряны пакеты снимка',conflicting_packets:'пакеты противоречат друг другу',source_limit:'ограничено число сохранённых заданий',invalid_rows:'состав строк не согласован',source_unavailable:'источник недоступен'};
  const unavailable = {1:'адаптер реестра не подключён',2:'запрос завершился ошибкой',3:'истекло время ожидания запроса',4:'источник вернул некорректные данные'};
  const duration = v => v == null ? 'неизвестно' : `${v} мс`;
  const moment = s => s.ObservedUnixMS == null ? `${s.ObservedElapsedMS} мс от загрузки устройства` : new Date(s.ObservedUnixMS).toLocaleString();
  const initialSnapshot = registryLatestSnapshotIndex(snapshots);
  let snapshotPage = Math.floor(Math.max(0, initialSnapshot) / snapshotPageSize);
  const renderSelector = () => {
    const range = registrySnapshotRange(snapshots.length, snapshotPage);
    snapshotPage = range.page;
    select.replaceChildren();
    for (let index = range.start; index < range.end; index++) {
      const s = snapshots[index];
      const option = document.createElement('option');
      option.value = index;
      option.textContent = `${s.Process || 'процесс неизвестен'} · ${moment(s)} · снимок ${s.SnapshotID}`;
      select.append(option);
    }
    select.value = Math.max(range.start, range.end - 1);
    root.querySelector('[data-registry-snap-page]').textContent = `Группа снимков ${range.page + 1} из ${range.pages}`;
  };
  renderSelector();
  if (initialSnapshot >= 0) select.value = initialSnapshot;
  let page = 0;
  const constraints = row => {
    const names = ['зарядка','заряд батареи достаточен','достаточно места','простой устройства','изменение URI','особый запрос сети'];
    return [`сеть: ${row.NetworkType || 'неизвестно'}`, ...names.filter((_, bit) => row.Constraints & (1 << bit))].join('; ');
  };
  let selectedJob;
  let selectedJobScope='';
  const attemptsButton = root.querySelector('[data-registry-attempts]');
  const clearJobSelection = () => {
    selectedJob = null; selectedJobScope = ''; attemptsButton.disabled = true; card.textContent = '';
  };
  attemptsButton.addEventListener('click', () => {
    if (selectedJob) document.dispatchEvent(new CustomEvent('jh-select-attempts',{detail:{scope:selectedJobScope,uuid:selectedJob.UUID}}));
  });
  const details = (snapshot, row) => {
    selectedJob = row;
    selectedJobScope = snapshot.Scope;
    attemptsButton.disabled = !document.querySelector('[data-worker-attempts]');
    const history = registryJobHistory(snapshots, snapshot, row.UUID);
    const historyRows = history.observations.map(({snapshot: s, job: j}) => ({
      'Наблюдение': moment(s), 'Состояние': states[j.State], 'Попытка по WorkInfo': j.RunAttempt,
      'Поколение по WorkInfo': j.Generation, 'Полнота снимка': s.Complete,
      'Период': j.PeriodMS, 'Flex': j.FlexMS, 'Раннее допустимое время': j.EarliestEligibleUnixMS,
    }));
    const chain = window.jhWorkChainForJob ? window.jhWorkChainForJob(chainEdges,snapshot.Scope,row.UUID) : null;
    const roots = window.jhWorkChainRootsForJob ? window.jhWorkChainRootsForJob(chainRoots,snapshot.Scope,row.UUID) : null;
    card.textContent = JSON.stringify({
      'UUID': row.UUID, 'Процесс наблюдателя': snapshot.Process, 'Область записи': snapshot.Scope,
      'Наблюдение': moment(snapshot), 'Состояние': states[row.State],
      'Фактическое начало выполнения': 'WorkInfo его не раскрывает; RUNNING не восстанавливает неизвестный старт',
      'Класс, backoff, expedited, quota policy': 'не доступны через этот источник WorkInfo',
      'Наблюдённые вызовы построения цепочки': roots && roots.observations.length ?
        {'Вызовы':roots.observations.map(v=>({
          'Unique name':v.Root.HasUnique?v.Root.UniqueName:'не задано',
          'Политика':chainPolicies[v.Root.Policy]||'неизвестно', 'Источник':v.Root.CallerSource,
          'Время наблюдения':v.TimeMS,
        })), 'Пропущено из карточки':roots.omitted,
          'Основание':'beginWith / beginUniqueWork вернул WorkContinuation; постановка не подтверждена'} :
        'не наблюдались; unique name и политика через WorkInfo неизвестны',
      'Наблюдённые зависимости цепочки': chain && (chain.incoming.length || chain.outgoing.length) ?
        {'Предшественники':chain.incoming,'Зависимые задания':chain.outgoing,
          'Пропущено из карточки':chain.incomingOmitted+chain.outgoingOmitted,
          'Основание':'публичный вызов WorkContinuation.then; постановка не подтверждена'} :
        'нет наблюдённых рёбер в этой сессии; полный граф неизвестен',
      'Причина прежней остановки': row.StopReason,
      'Заявленные ограничения': constraints(row), 'Триггеры URI': row.ContentTriggers,
      'Progress: только метаданные': row.Progress, 'Output: только метаданные': row.Output,
      'История progress по снимкам WorkInfo (метаданные; одинаковые размеры не доказывают отсутствие изменений)':
        registryProgressTimeline(history),
      'Пропущено тегов': row.OmittedTags, 'Пропущено URI': row.OmittedContentTriggers,
      'Пропущено ключей progress/output': [row.OmittedProgressKeys, row.OmittedOutputKeys],
      'Усечено текстовых полей': row.TruncatedFields, 'Всего наблюдений до выбранного снимка': history.total,
      'Последние 200 наблюдений до выбранного снимка': historyRows,
    }, null, 2);
  };
  const draw = () => {
    const s = snapshots[Number(select.value)];
    if (!s) return;
    const counts = states.map((name, state) => `${name}: ${(s.Jobs || []).filter(j => j.State === state).length}`).join('; ');
    const coverage = s.Complete ? 'Получены все задания снимка.' : `Неполные данные: ${reasons[s.CoverageReason] || 'охват неизвестен'}.`;
    const clock = s.ObservedUnixMS == null ? ' Календарный якорь отсутствует; порядок снимков разных сессий неизвестен.' : '';
    const age = s.AgeAtCaptureMS == null ? 'Возраст относительно последнего среза журнала неизвестен.' : `Возраст к последнему срезу журнала: ${s.AgeAtCaptureMS} мс.`;
    info.textContent = s.Unavailable ? `Реестр неизвестен: ${unavailable[s.Unavailable] || 'неизвестная причина'}. ${age}` :
      `${coverage} В ответе API: ${s.TotalObserved}; сохранено: ${(s.Jobs || []).length}. Пакеты: ${s.ReceivedPackets}/${s.ExpectedPackets}. ${counts}. ${age}${clock} Пропущено пакетов в анализе: ${data.DroppedPackets || 0}.`;
    const query = search.value.toLowerCase();
    const rows = (s.Jobs || []).filter(j => (stateFilter.value === '' || String(j.State) === stateFilter.value) &&
      `${j.UUID} ${(j.Tags || []).join(' ')} ${j.NetworkType}`.toLowerCase().includes(query));
    const pages = Math.max(1, Math.ceil(rows.length / 50));
    page = Math.min(page, pages - 1);
    body.replaceChildren();
    rows.slice(page * 50, (page + 1) * 50).forEach(row => {
      const tr = document.createElement('tr');
      const link = document.createElement('button');
      link.type = 'button'; link.textContent = row.UUID;
      link.addEventListener('click', () => details(s, row));
      const id = document.createElement('td'); id.append(link); tr.append(id);
      const cells = [states[row.State],`${row.RunAttempt} / ${row.Generation}`,(row.Tags || []).join(', '),
        duration(row.InitialDelayMS),row.PeriodMS == null ? 'одноразовое' : `${row.PeriodMS} / ${row.FlexMS} мс`,
        row.EarliestEligibleUnixMS == null ? 'неизвестно' : new Date(row.EarliestEligibleUnixMS).toLocaleString(),constraints(row)];
      cells.forEach(value => { const td = document.createElement('td'); td.textContent = value; tr.append(td); });
      body.append(tr);
    });
    pageLabel.textContent = `Страница ${page + 1} из ${pages}; найдено ${rows.length}.`;
  };
  select.addEventListener('change', () => { page = 0; clearJobSelection(); draw(); });
  search.addEventListener('input', () => { page = 0; draw(); });
  stateFilter.addEventListener('change', () => { page = 0; draw(); });
  root.querySelector('[data-registry-prev]').addEventListener('click', () => { page = Math.max(0, page - 1); draw(); });
  root.querySelector('[data-registry-next]').addEventListener('click', () => { page++; draw(); });
  root.querySelector('[data-registry-snap-prev]').addEventListener('click', () => {
    snapshotPage--; renderSelector(); page = 0; clearJobSelection(); draw();
  });
  root.querySelector('[data-registry-snap-next]').addEventListener('click', () => {
    snapshotPage++; renderSelector(); page = 0; clearJobSelection(); draw();
  });
  document.addEventListener('jh-select-job', event => {
    const found = registryFindJob(snapshots,event.detail.scope,event.detail.uuid);
    if (found < 0) {info.textContent='Наблюдение этого UUID в данной сессии реестра не найдено.';root.scrollIntoView({block:'start'});return;}
    snapshotPage=Math.floor(found/snapshotPageSize);renderSelector();select.value=found;search.value=event.detail.uuid;stateFilter.value='';page=0;
    draw();details(snapshots[found],snapshots[found].Jobs.find(j => j.UUID === event.detail.uuid));
    card.closest('details').open=true;root.scrollIntoView({block:'start'});
  });
  draw();
})();
