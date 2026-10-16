// Calendar anchors may move across segments; keep all observed points inside the plot.
const threadTimeDomain = p => {
  let first = Infinity, end = -Infinity;
  const add = value => { if (Number.isFinite(value)) {first=Math.min(first,value);end=Math.max(end,value);} };
  add(p.StartAtMS);add(p.LastAtMS);add(p.ObservationEndMS);
  (p.Snapshots || []).forEach(point=>add(point.TimeMS));
  if (!Number.isFinite(first)) first=end=0;
  return {first,end,span:Math.max(1,end-first)};
};
const threadObservedDuration = h => {
  const points=h.Observations || [], first=points[0]?.ElapsedMS, last=points.at(-1)?.ElapsedMS;
  if (Number.isSafeInteger(first) && Number.isSafeInteger(last)) return last>=first ? last-first : null;
  return Number.isFinite(h.FirstMS) && Number.isFinite(h.LastMS) && h.LastMS>=h.FirstMS ? h.LastMS-h.FirstMS : null;
};
const threadAnnotationKinds = {operation_start:'Начало операции',operation_finish:'Завершение операции',worker_start:'Запуск воркера',ui_visibility:'Видимость UI',ui_stall:'Зависание UI',ui_jank_window:'Проблемное UI-окно',screen_observation:'Экран при наблюдении'};
const threadAnnotationView = (items,scope,kind,page) => {
  const values=items.filter(v=>v.Scope===scope && (!kind || v.Kind===kind)).sort((a,b)=>a.TimeMS-b.TimeMS);
  const pages=Math.max(1,Math.ceil(values.length/50));page=Math.max(0,Math.min(page,pages-1));
  return {rows:values.slice(page*50,(page+1)*50),total:values.length,page,pages};
};
const threadAnnotationSnapshot = (points,time) => {
  let best=null;
  points.forEach((s,point)=>{
    if(s.TimeBasis!=='observation_clock')return;
    const distance=Math.abs(s.TimeMS-time);
    if(!best || distance<best.distance)best={point,distance};
  });
  return best;
};
const threadSnapshotDiff = (before, after) => {
  const key = r => String(r.tid)+':'+String(r.start_ticks);
  const old = new Map(before.map(r=>[key(r),r])), now = new Map(after.map(r=>[key(r),r]));
  const result={added:[],gone:[],renamed:[],changed:[],javaRenamed:[],javaChanged:[]};
  now.forEach((r,k)=>{
    const prior=old.get(k);
    if(!prior) {result.added.push(r.name+' ['+r.tid+']');return;}
    if(r.name!==prior.name) result.renamed.push(prior.name+' → '+r.name);
    if(r.os_state!==prior.os_state) result.changed.push(r.name+': '+prior.os_state+' → '+r.os_state);
    if(r.java && prior.java && r.java.id===prior.java.id) {
      if(r.java.name!==prior.java.name) result.javaRenamed.push(prior.java.name+' → '+r.java.name);
      if(r.java.state!==prior.java.state) result.javaChanged.push(r.java.name+': '+prior.java.state+' → '+r.java.state);
    }
  });
  old.forEach((r,k)=>{if(!now.has(k)) result.gone.push(r.name+' ['+r.tid+']');});
  return result;
};
const threadJavaLossText = snapshot => snapshot.version < 2 ? 'неизвестно: schema1' : String(snapshot.dropped_java_identities);
const threadOwned = row => row.java?.owned_by_jank_hunter === true;
// /proc task names are limited to 15 bytes. Match only names used by our own
// thread factories (including their kernel truncation), never a broad prefix.
const collectorThreadNames = new Set([
  'JankHunterEvents', 'JankHunterGraph', 'JankHunterMainWatchdog',
  'JankHunterMaintenance', 'JankHunterMetricDrain', 'JankHunterRetentionStop',
  'JankHunterSnapshot', 'JankHunterSnapshotCapture', 'JankHunterWriter'
].flatMap(name => [name, name.slice(0, 15)]));
const threadCollectorNameHint = row => row.java?.owned_by_jank_hunter !== false && collectorThreadNames.has(row.name || '');
const threadCollectorJavaSource = row => (row.java?.source || '').startsWith('Jank Hunter ');
const threadCollectorLikely = row => threadOwned(row) || threadCollectorNameHint(row) || threadCollectorJavaSource(row);
const threadGenerationKey = row => String(row.tid) + ':' + String(row.start_ticks);
const collectorGenerationsCache = new WeakMap();
const threadCollectorGenerations = process => {
  if (!process || typeof process !== 'object') return new Map();
  let generations = collectorGenerationsCache.get(process);
  if (generations) return generations;
  generations = new Map();
  (process.Histories || []).forEach(history => {
    const rows = (history.Observations || []).map(point => point.Row);
    const evidence = rows.some(row => threadOwned(row) || threadCollectorJavaSource(row)) ? 'confirmed' :
      rows.some(threadCollectorNameHint) ? 'hint' : '';
    if (evidence) generations.set(String(history.TID) + ':' + String(history.StartTicks), evidence);
  });
  collectorGenerationsCache.set(process, generations);
  return generations;
};
const threadSource = row => row.java?.source || '';
const sourceRoots = value => (value || '').split(/[;,\s]+/).map(root => root.trim().replace(/\.$/, '')).filter(Boolean);
const sourceHasRoot = (source, root) => source === root || source.startsWith(root + '.') || source.startsWith(root + '$');
const sourceOrigin = (source, roots) => {
  if (!source || source === 'unknown') return 'unknown';
  if (source.startsWith('Jank Hunter ')) return 'collector';
  if ((roots || []).some(root => sourceHasRoot(source, root))) return 'app';
  if (['android.', 'com.android.', 'dalvik.', 'libcore.', 'sun.', 'jdk.'].some(root => source.startsWith(root)) || source.startsWith('java.')) return 'system';
  if (['kotlin.', 'kotlinx.', 'androidx.', 'com.google.', 'okhttp3.', 'okio.', 'io.reactivex.'].some(root => source.startsWith(root))) return 'library';
  return 'unknown';
};
const threadOriginCategory = (row, roots, collectorGenerations) =>
  threadOwned(row) || threadCollectorJavaSource(row) || collectorGenerations?.get(threadGenerationKey(row)) === 'confirmed' ?
    'collector' : sourceOrigin(threadSource(row), roots);
const originLabel = category => ({app:'Наблюдался код приложения',system:'Наблюдался системный код',library:'Наблюдался код библиотеки',collector:'Jank Hunter',unknown:'Код не классифицирован'})[category] || 'Код не классифицирован';
const standardThreadName = /^(?:Signal Catcher|Jit thread pool|HeapTaskDaemon|ReferenceQueueD|FinalizerDaemon|FinalizerWatchd|binder:|Binder:|RenderThread|hwuiTask|Profile Saver|ADB-JDWP|perfetto_|DefaultDispatch|OkHttp |Okio |Conscrypt|Firebase-|firebase-|RxCached|RxComputation|RxSingle|GoogleApiHandle|ThreadPoolForeg|process reaper|LeakCanary-|TracingMuxer|ProtoLogBackgro|SurfaceSyncGrou|FrameMetricsAgg|SharedPreferenc|JankHunter)/i;
const threadNameLooksStandard = row => standardThreadName.test(row.name || '');
const threadNameHint = row => {
  const name = row.name || '';
  if (/^DefaultDispatch/i.test(name)) return 'имя похоже на диспетчер корутин Kotlin';
  if (/^OkHttp TaskRunn/i.test(name)) return 'имя похоже на OkHttp TaskRunner';
  if (/^Okio Watchdog/i.test(name)) return 'имя похоже на Okio Watchdog';
  if (/^Conscrypt/i.test(name)) return 'имя похоже на Conscrypt';
  if (/^(?:RxCached|RxComputation|RxSingle)/i.test(name)) return 'имя похоже на пул RxJava';
  if (/^(?:Firebase-|firebase-)/i.test(name)) return 'имя похоже на Firebase';
  if (/^(?:binder:|Binder:)/.test(name)) return 'имя похоже на поток Android Binder';
  if (/^(?:Signal Catcher|Jit thread pool|HeapTaskDaemon|ReferenceQueueD|FinalizerDaemon|FinalizerWatchd|process reaper)/i.test(name)) return 'имя похоже на служебный поток Android/ART';
  if (threadNameLooksStandard(row)) return 'имя похоже на системный или библиотечный поток';
  return '';
};
const threadMatchesOrigin = (row, origin, roots, collectorGenerations) => {
  if (!origin) return true;
  const category=threadOriginCategory(row,roots,collectorGenerations);
  if (origin==='app-candidates') return category==='app' || (category==='unknown' && !threadNameLooksStandard(row));
  if (origin==='standard-names') return category==='unknown' && threadNameLooksStandard(row);
  return category===origin;
};
const threadHistorySource = (history, query) => threadSource((history.Observations || []).find(o => threadSource(o.Row).toLowerCase().includes(query.toLowerCase()) && threadSource(o.Row))?.Row || {});
const threadFactoryIndex = (items, scope, limit = 10000) => {
  const result={byID:new Map(),omitted:0};
  (items || []).forEach(item=>{
    const o=item.Observation;
    if(item.Scope!==scope || o?.stage!==3 || typeof o.java_id!=='string' || !o.factory || typeof o.observed_elapsed_nanos!=='string')return;
    let at;
    try { at=BigInt(o.observed_elapsed_nanos); } catch { return; }
    if(at<=0n)return;
    let entry=result.byID.get(o.java_id);
    if(!entry){if(result.byID.size>=limit){result.omitted++;return;}entry={factories:[],ambiguousAt:null};result.byID.set(o.java_id,entry);}
    const prior=entry.factories.find(value=>value.name===o.factory);
    if(prior){if(at<prior.at)prior.at=at;return;}
    if(entry.factories.length>=4){if(entry.ambiguousAt==null || at<entry.ambiguousAt)entry.ambiguousAt=at;result.omitted++;return;}
    entry.factories.push({name:o.factory,at});
  });
  return result;
};
const threadFactoryText = (index, row, scanElapsedMS) => {
  if(typeof row.java?.id!=='string' || !Number.isSafeInteger(scanElapsedMS) || scanElapsedMS<=0)return 'фабрика неизвестна: подтверждённой связи до снимка нет';
  const entry=index.byID.get(row.java.id);
  if(!entry)return 'фабрика неизвестна: подтверждённого возврата нет';
  const boundary=BigInt(scanElapsedMS)*1000000n;
  const names=entry.factories.filter(factory=>factory.at<boundary);
  if(entry.ambiguousAt!=null && entry.ambiguousAt<boundary)return names.length ? 'несколько фабрик вернули этот Java Thread; принадлежность пулу неизвестна' : 'индекс фабрик неполон до снимка; принадлежность пулу неизвестна';
  if(!names.length)return 'фабрика неизвестна до начала снимка';
  if(names.length>1)return 'несколько фабрик вернули этот Java Thread; принадлежность пулу неизвестна';
  return 'фабрика '+names[0].name+' вернула этот Java Thread до снимка; принадлежность пулу неизвестна';
};
const threadInScope = (row, excludeOwned, sourceQuery, origin = '', roots = [], collectorGenerations) =>
  (!excludeOwned || (!threadCollectorLikely(row) && !collectorGenerations?.has(threadGenerationKey(row)))) &&
  (!sourceQuery || threadSource(row).toLowerCase().includes(sourceQuery.toLowerCase())) &&
  threadMatchesOrigin(row, origin, roots, collectorGenerations);
const threadSearchMatches = (row, query) => !query || threadSearchText(row).includes(query.toLowerCase());
const threadVisibleRows = (rows, excludeOwned, sourceQuery = '', origin = '', roots = [], collectorGenerations) =>
  sourceQuery || excludeOwned || origin ? (rows || []).filter(row => threadInScope(row, excludeOwned, sourceQuery, origin, roots, collectorGenerations)) : (rows || []);
const threadChartCounts = (snapshots, excludeOwned, sourceQuery = '', origin = '', roots = [], collectorGenerations, query = '') => (snapshots || []).map(s => {
  const rows=s.Snapshot.threads || [];
  return sourceQuery || excludeOwned || origin || query ? rows.reduce((count,row)=>count+
    (threadInScope(row,excludeOwned,sourceQuery,origin,roots,collectorGenerations) && threadSearchMatches(row,query)?1:0),0) : rows.length;
});
const threadLifelineView = (histories, matches, limit = 200) => {
  const result = [];
  for (const history of histories || []) {
    const points = [], runs = [];
    let run = [];
    for (const point of history.Observations || []) {
      if (matches(point.Row)) { points.push(point); run.push(point); }
      else if (run.length) { runs.push(run); run = []; }
    }
    if (run.length) runs.push(run);
    if (!points.length) continue;
    result.push({history, points, runs});
    if (result.length >= limit) break;
  }
  return result;
};
const threadOwnershipText = row => threadOwned(row) ? 'поток Jank Hunter: подтверждён регистрацией на выполняющемся TID' :
  threadCollectorNameHint(row) ? 'внутренний поток Jank Hunter предположен по известному имени; Java-принадлежность не подтверждена' :
  'принадлежность Jank Hunter не подтверждена';
const threadSearchText = r => `${r.name} ${r.tid} ${r.java?.id || ''} ${r.java?.name || ''} ${r.java?.source || ''}`.toLowerCase();
const threadJavaText = java => java == null ? 'неизвестно: нет подтверждённой связи' : `Java ID ${java.id}; ${java.name}; ${java.state}; daemon=${java.daemon}; ${java.owned_by_jank_hunter === true ? 'регистрация выполняющегося потока' : 'первый run-хук'} ${java.first_observed_elapsed_nanos} нс elapsedRealtime; ${java.source}; создание и start неизвестны`;
const threadMainText = history => history?.MainThread == null ? 'главный поток неизвестен: PID отсутствует' : history.MainThread ? 'главный поток (TID = PID)' : 'не главный поток';
const threadOriginText = history => {
  const start=history?.KernelStart;
  if (!start) return 'старт неизвестен: нет допустимого системного времени или частоты тиков';
  const calendar=start.FromUnixMS==null || start.ByUnixMS==null ? 'календарное время неизвестно' : `${start.FromUnixMS}-${start.ByUnixMS} мс по календарному якорю`;
  const relation=start.BeforeCollection==null ? 'отношение к зафиксированной границе сбора неизвестно: нет границы или интервалы пересекаются' : start.BeforeCollection ? 'существовал до зафиксированной границы сбора' : 'появился после зафиксированной границы сбора';
  return `${start.FromElapsedMS}-${start.ByElapsedMS} мс от загрузки; ${calendar}; ${relation}. Источник: /proc stat, точность системного тика; это старт задачи ОС, не создание Java Thread.`;
};
const threadCPUText = cpu => cpu == null ? 'неизвестно' : `${cpu.CorePercent.toFixed(1)}% ядра за ${cpu.ElapsedMS} мс; тик ${cpu.TickMS.toFixed(2)} мс`;
const threadSelectedCPUText = (cpu, hasPrior, priorSelected) => cpu == null ? threadCPUText(cpu) :
  threadCPUText(cpu) + (priorSelected ? '' : hasPrior ?
    '; дельта не вошла в сводку: предыдущее наблюдение вне фильтра' :
    '; дельта не вошла в сводку: предыдущего наблюдения нет');
// A lifecycle event carries a Linux TID, but no kernel generation. Navigation
// selects a nearby candidate only; TID reuse prevents an exact generation join.
const threadEndpointObservation = (processes, endpoint) => {
  if (!endpoint.tid || endpoint.time == null || !endpoint.scope) return null;
  let best = null;
  (processes || []).forEach((p, process) => {
    if (p.Scope !== endpoint.scope) return;
    (p.Snapshots || []).forEach((s, point) => {
      const radius = s.Snapshot.interval_ms + s.Snapshot.scan_duration_ms;
      if (s.TimeBasis !== 'observation_clock' || !(s.Snapshot.interval_ms > 0)) return;
      const distance = Math.abs(s.TimeMS - endpoint.time);
      if (distance > radius || (best && distance >= best.distance)) return;
      if (!(s.Snapshot.threads || []).some(r => String(r.tid) === endpoint.tid)) return;
      best = {process,point,distance};
    });
  });
  return best;
};
const threadAnalysisCache=new WeakMap();
const threadSnapshotPageSize=100;
const threadDefaultProcessIndex = processes => {
  let best = 0, bestComplete = -1, bestTotal = -1;
  (processes || []).forEach((process, index) => {
    const snapshots = process.Snapshots || [];
    const complete = snapshots.reduce((count, point) => count + (point.Snapshot?.complete === true ? 1 : 0), 0);
    if (complete > bestComplete || (complete === bestComplete && snapshots.length > bestTotal)) {
      best = index; bestComplete = complete; bestTotal = snapshots.length;
    }
  });
  return best;
};
const threadAnalysisData=root=>{
  const section=root.closest('[data-thread-analysis]') || root;
  let data=threadAnalysisCache.get(section);
  if(!data){
    data=JSON.parse(section.querySelector('[data-thread-json]').textContent);
    threadAnalysisCache.set(section,data);
  }
  return data;
};
if (typeof module !== 'undefined') module.exports = {threadTimeDomain,threadObservedDuration,threadMainText,threadOriginText,threadAnnotationView,threadAnnotationSnapshot,threadSnapshotDiff,threadCPUText,threadSelectedCPUText,threadEndpointObservation,threadJavaText,threadSearchText,threadSearchMatches,threadJavaLossText,threadOwned,threadCollectorNameHint,threadCollectorLikely,threadCollectorGenerations,threadInScope,threadVisibleRows,threadChartCounts,threadLifelineView,threadOwnershipText,threadSource,threadHistorySource,threadFactoryIndex,threadFactoryText,threadAnalysisData,threadDefaultProcessIndex,sourceRoots,sourceOrigin,threadOriginCategory,originLabel,threadNameHint,threadNameLooksStandard,threadMatchesOrigin};

// Bounded thread views: never materialize the complete history as DOM rows.
if (typeof document !== 'undefined') document.querySelectorAll('[data-thread-analysis]').forEach(function (root) {
  const data = threadAnalysisData(root);
  const coroutineThreads=new Set((data.CoroutineSegments || []).flatMap(segment=>[
    segment.Scope+'|pair|'+segment.EntryJavaThreadID+'|'+segment.EntryTID,
    segment.Scope+'|pair|'+segment.ExitJavaThreadID+'|'+segment.ExitTID,
    segment.Scope+'|t|'+segment.EntryTID,segment.Scope+'|t|'+segment.ExitTID]));
  const resourceThreads=new Set((data.Resources || []).map(row=>row.Scope+'|'+row.CompletionTID));
  const get = name => root.querySelector('[data-thread-' + name + ']');
  const process = get('process'), point = get('point'), before = get('before'), search = get('overview-search'), excludeOwned = get('exclude-owned'), sourceFilter = get('source'), originFilter = get('origin'), appRoots = get('app-roots');
  const rawDetails = root.querySelector('.thread-raw-details');
  const lifeDetails = root.querySelector('.thread-lifeline-details');
  let page = 0, annotationPage = 0, linkedTID = null, factoryScope = null, factories = null;
  initThreadLifecycle(root, data);
  (data.Processes || []).forEach((p, i) => { const option = document.createElement('option'); option.value = i; option.textContent = p.Process + ' / ' + p.Scope; process.appendChild(option); });
  process.value = String(threadDefaultProcessIndex(data.Processes));
  appRoots.value = [...new Set((data.Processes || []).map(p => p.Process.split(':')[0]).filter(Boolean))].join(', ');
  const text = (name, value) => { get(name).textContent = value; };
  const key = row => String(row.tid) + ':' + String(row.start_ticks);
  function annotations(p) {
    const view=threadAnnotationView(data.Annotations || [],p.Scope,get('annotation-kind').value,annotationPage);
    annotationPage=view.page;const body=get('annotation-rows');body.replaceChildren();
    view.rows.forEach(mark=>{
      const row=document.createElement('tr'),cell=document.createElement('td'),button=document.createElement('button');
      button.type='button';button.textContent=mark.TimeMS+' мс ('+(mark.TimeBasis==='observation_clock'?'календарный якорь':'монотонное время записи')+')';
      const match=mark.TimeBasis==='observation_clock' ? threadAnnotationSnapshot(p.Snapshots,mark.TimeMS) : null;button.disabled=!match;
      button.addEventListener('click',()=>{point.value=match.point;linkedTID=null;render();text('link','Выбран ближайший снимок: расстояние '+match.distance+' мс. Состав потоков в момент отметки неизвестен; совпадение времени не доказывает причину.');});
      cell.appendChild(button);row.appendChild(cell);
      [threadAnnotationKinds[mark.Kind] || mark.Kind,mark.Label,mark.Screen || 'неизвестно',mark.OperationID || 'неизвестно',mark.DurationMS ? mark.DurationMS+' мс (длительность события, не потока)' : 'точка наблюдения'].forEach(value=>{const td=document.createElement('td');td.textContent=value;row.appendChild(td);});
      body.appendChild(row);
    });
    text('annotation-page','Страница '+(view.page+1)+' из '+view.pages+'; отметок '+view.total+'; пропущено по лимиту CLI '+(data.DroppedAnnotations || 0));
  }
  function chart(p) {
    const canvas = get('chart'), ctx = canvas.getContext('2d'); ctx.clearRect(0, 0, canvas.width, canvas.height);
    const counts = threadChartCounts(p.Snapshots, excludeOwned.checked, sourceFilter.value.trim(), originFilter.value, sourceRoots(appRoots.value), threadCollectorGenerations(p), search.value.trim());
    const {first,end,span} = threadTimeDomain(p), peak = counts.reduce((max, count) => Math.max(max, count), 1);
    ctx.fillStyle = '#8796ae'; ctx.font = '12px sans-serif'; ctx.fillText('наблюдаемые потоки в выбранной области: 0 - ' + peak, 10, 14);
    ctx.fillText(first + ' - ' + end + ' мс', 10, 185);
    if (p.ObservationEndMS != null) {ctx.fillStyle='#e6edf3';ctx.fillRect(20+(p.ObservationEndMS-first)/span*950,20,2,149);}
    (data.Annotations || []).forEach(mark=>{
      if(mark.TimeBasis!=='observation_clock' || mark.Scope!==p.Scope || mark.TimeMS<first || mark.TimeMS>end)return;
      const x=20+(mark.TimeMS-first)/span*950;
      ctx.fillStyle=mark.Kind==='worker_start'?'#e0b55b':mark.Kind.startsWith('ui_')?'#dd7384':'#8bc99f';
      ctx.fillRect(x,160,2,9);
    });
    p.Snapshots.forEach((s, i) => { ctx.fillStyle = i === Number(point.value) ? '#ff8f50' : (s.Snapshot.complete ? '#56b6e8' : '#dd7384'); ctx.beginPath(); ctx.arc(20 + (s.TimeMS - first) / span * 950, 160 - counts[i] / peak * 135, 3, 0, Math.PI * 2); ctx.fill(); });
  }
  function render() {
    const p = (data.Processes || [])[Number(process.value) || 0]; if (!p || !p.Snapshots.length) return;
    const collectorGenerations = threadCollectorGenerations(p);
    if(factoryScope!==p.Scope){factoryScope=p.Scope;factories=threadFactoryIndex(data.Lifecycle || [],p.Scope);}
    text('factory-quality','Подтверждены только возвраты Thread из фабрики в этой сессии; принадлежность пулу и выполнение в нём неизвестны. Не сохранено в индексе по лимиту: '+factories.omitted+'.');
    point.max = before.max = p.Snapshots.length - 1;
    const selected = Math.min(Number(point.value), p.Snapshots.length - 1), s = p.Snapshots[selected];
    const b = p.Snapshots[Math.max(0, Math.min(Number(before.value) || 0, selected))];
    const query = search.value.trim().toLowerCase();
    const roots = sourceRoots(appRoots.value), sourceQuery = sourceFilter.value.trim(), selectedOrigin = originFilter.value;
    const matches = row => threadInScope(row, excludeOwned.checked, sourceQuery, selectedOrigin,
      roots, collectorGenerations) && threadSearchMatches(row, query);
    const visible = rows => (rows || []).filter(matches);
    const rows = visible(s.Snapshot.threads).filter(r => linkedTID === null || String(r.tid) === linkedTID);
    page = Math.max(0, Math.min(page, Math.ceil(rows.length / threadSnapshotPageSize) - 1));
    const cpuByThread = new Map(), historyByThread = new Map();
    (p.Histories || []).forEach(h => {
      historyByThread.set(String(h.TID)+':'+String(h.StartTicks),h);
      const index = h.Observations.findIndex(o => o.TimeMS === s.TimeMS && o.ElapsedMS === s.Snapshot.elapsed_ms);
      if (index >= 0) {
        const prior = index > 0 ? h.Observations[index - 1] : null;
        cpuByThread.set(key(h.Observations[index].Row), {cpu: h.Observations[index].CPU,
          hasPrior: prior != null, priorSelected: prior != null && matches(prior.Row)});
      }
    });
    const body = get('rows'); body.replaceChildren();
    rows.slice(page * threadSnapshotPageSize, (page + 1) * threadSnapshotPageSize).forEach(r => {
      const tr = document.createElement('tr');
      const cpu = cpuByThread.get(key(r));
      [r.name + ' · TID ' + r.tid, originLabel(threadOriginCategory(r,roots,collectorGenerations)), r.os_state + ' · ' + threadSelectedCPUText(cpu?.cpu, cpu?.hasPrior, cpu?.priorSelected), threadSource(r) || 'источник неизвестен'].forEach(v => { const td = document.createElement('td'); td.textContent = v; tr.appendChild(td); });
      const detailCell=document.createElement('td'), detail=document.createElement('details'), summary=document.createElement('summary');
      summary.textContent='Данные';detail.append(summary);
      const detailText=document.createElement('div');detailText.className='thread-row-detail';
      [threadOwnershipText(r),threadJavaText(r.java),threadFactoryText(factories,r,s.Snapshot.elapsed_ms),threadMainText(historyByThread.get(key(r))),r.start_ticks+' тиков; '+threadOriginText(historyByThread.get(key(r))), 'CPU user / system: '+r.user_ticks+' / '+r.system_ticks, 'Приоритет / nice: '+r.priority+' / '+r.nice].forEach(value=>{const item=document.createElement('p');item.textContent=value;detailText.append(item);});
      detail.append(detailText);detailCell.append(detail);tr.append(detailCell);
      const cell = document.createElement('td'), button = document.createElement('button');
      button.type = 'button'; button.textContent = 'Попытки рядом';
      button.disabled = s.TimeBasis !== 'observation_clock' || !(s.Snapshot.interval_ms > 0) || (!(root.dataset.workerHref && root.dataset.workerAttemptsAvailable==='true') && !document.querySelector('[data-worker-attempts]'));
      button.addEventListener('click', () => {
        const detail={scope:p.Scope,tid:String(r.tid),time:s.TimeMS,radius:s.Snapshot.interval_ms+s.Snapshot.scan_duration_ms};
        if(root.dataset.workerHref)window.jhNavigateSelection(root.dataset.workerHref+'#work-attempts','jh-select-thread-attempts',detail);
        else document.dispatchEvent(new CustomEvent('jh-select-thread-attempts',{detail}));
      });
      cell.appendChild(button);
      const coroutineButton=document.createElement('button');coroutineButton.type='button';coroutineButton.textContent='Корутины';
      coroutineButton.disabled=r.java?.id ?
        !coroutineThreads.has(p.Scope+'|pair|'+r.java.id+'|'+String(r.tid)):
        !coroutineThreads.has(p.Scope+'|t|'+String(r.tid));
      coroutineButton.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-coroutines',
        {detail:{scope:p.Scope,javaID:r.java?.id || '',tid:String(r.tid)}})));
      cell.appendChild(coroutineButton);
      const resourceButton=document.createElement('button');resourceButton.type='button';resourceButton.textContent='Операции';
      resourceButton.disabled=!resourceThreads.has(p.Scope+'|'+String(r.tid));
      resourceButton.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-thread-resources',
        {detail:{scope:p.Scope,tid:String(r.tid)}})));
      cell.appendChild(resourceButton);tr.appendChild(cell);body.appendChild(tr);
    });
    text('page', ' ' + (page + 1) + ' / ' + Math.max(1, Math.ceil(rows.length / threadSnapshotPageSize)) + ' · найдено ' + rows.length);
    text('detail', 'Снимок #' + selected + ': ' + visible(s.Snapshot.threads).length + ' из ' + (s.Snapshot.threads || []).length + ' потоков · ' + s.TimeMS + ' мс · ' + (s.Snapshot.complete ? 'полный снимок' : 'неполный: ' + s.Snapshot.reason) + (originFilter.value==='app-candidates' ? ' · Отбор по коду приложения или нетипичному имени не подтверждает создателя потока.' : ''));
    const {added,gone,renamed,changed,javaRenamed,javaChanged} = threadSnapshotDiff(visible(b.Snapshot.threads),visible(s.Snapshot.threads));
    const bounded = values => values.slice(0, 30).join(', ') + (values.length > 30 ? ' … (всего ' + values.length + ')' : '');
    text('diff', 'Между #' + before.value + ' и #' + selected + ': появились в снимке: ' + (bounded(added) || 'нет') + '; отсутствуют в снимке: ' + (bounded(gone) || 'нет') + '; переименования: ' + (bounded(renamed) || 'нет') + '; состояния ОС: ' + (bounded(changed) || 'нет') + '; Java-переименования: ' + (bounded(javaRenamed) || 'нет') + '; Java-состояния: ' + (bounded(javaChanged) || 'нет') + ((!s.Snapshot.complete || !b.Snapshot.complete) ? '. Один из снимков неполон - отсутствие не доказывает исчезновение.' : '') + (sourceFilter.value.trim() ? ' Изменение состава при фильтре источника может означать появление Java-связи, а не нового системного потока.' : ''));
    chart(p); annotations(p);
    if (lifeDetails?.open) {
      const lifelines = threadLifelineView(p.Histories, matches);
      const selectedSource = item => threadSource(item.points.find(o => threadSource(o.Row))?.Row || {});
      lifelines.sort((a,b)=>selectedSource(a).localeCompare(selectedSource(b)));
      const canvas = get('life'); canvas.height = Math.max(50, lifelines.length * 18 + 20); const ctx = canvas.getContext('2d'); ctx.font = '11px sans-serif';
      const {first,span} = threadTimeDomain(p), x = time => 220 + (time - first) / span * 750;
      lifelines.forEach(({history, points, runs}, i) => {
        const y = i * 18 + 15, source = selectedSource({points}) || 'источник неизвестен';
        ctx.fillStyle = '#a8b8cb'; ctx.fillText((source+' / '+points[0].Row.name+' / '+history.TID).slice(0, 38), 0, y);
        ctx.strokeStyle = '#6680a0'; ctx.setLineDash([3, 4]); ctx.beginPath();
        runs.forEach(run => {ctx.moveTo(x(run[0].TimeMS), y); ctx.lineTo(x(run.at(-1).TimeMS), y);});
        ctx.stroke();
        ctx.fillStyle = '#56b6e8'; ctx.beginPath();
        points.forEach(o => ctx.rect(x(o.TimeMS) - 1, y - 2, 3, 4));
        ctx.fill();
      });
    }
  }
  get('annotation-kind').addEventListener('input',()=>{annotationPage=0;render();});
  get('annotation-prev').addEventListener('click',()=>{annotationPage--;render();});
  get('annotation-next').addEventListener('click',()=>{annotationPage++;render();});
  process.addEventListener('input',()=>{annotationPage=0;});
  [process, point, before, search, sourceFilter, excludeOwned, originFilter, appRoots].forEach(el => el.addEventListener('input', () => { page = 0; linkedTID = null; text('link', ''); if (rawDetails?.open) render(); }));
  rawDetails?.addEventListener('toggle', () => { if (rawDetails.open) render(); });
  lifeDetails?.addEventListener('toggle', () => { if (lifeDetails.open && rawDetails.open) render(); });
  get('prev').addEventListener('click', () => { page--; render(); }); get('next').addEventListener('click', () => { page++; render(); });
  document.addEventListener('jh-select-thread', event => {
    const match = threadEndpointObservation(data.Processes, event.detail);
    if (!match) {
      text('link', 'Близких наблюдений этого TID в той же сессии нет. Короткоживущий поток мог пройти между снимками; другая сессия не используется.');
    } else {
      process.value = match.process; point.max = data.Processes[match.process].Snapshots.length - 1; point.value = match.point; before.value = match.point;
      search.value = '';originFilter.value='';sourceFilter.value='';
      search.dispatchEvent(new Event('input', {bubbles: true}));
      linkedTID = event.detail.tid; page = 0; rawDetails.open = true; render();
      text('link', 'Кандидат по TID и близости времени: расстояние ' + match.distance + ' мс. Поколение в событии попытки не записано; повторное использование TID не исключено. Это не доказательство непрерывного выполнения на потоке.');
    }
    (rawDetails?.open ? rawDetails : root).scrollIntoView({block:'start'});
  });
  document.addEventListener('jh-select-java-thread', event=>{
    const match=event.detail;
    const p=data.Processes?.[match.process],snapshot=p?.Snapshots?.[match.point];
    if(!p || p.Scope!==match.scope || !snapshot?.Snapshot?.threads?.some(row=>
      row.java?.id===match.javaID && String(row.tid)===match.tid))return;
    process.value=match.process;point.value=match.point;before.value=match.point;
    search.value='';originFilter.value='';sourceFilter.value='';
    search.dispatchEvent(new Event('input', {bubbles: true}));
    linkedTID=match.tid;page=0;rawDetails.open=true;render();
    text('link','Наблюдалась Java-связь с TID '+match.tid+' в ближайшем снимке, расстояние '+match.distance+
      ' мс от начала сегмента. Это кандидат по наблюдению, не доказательство состояния потока внутри всего сегмента.');
    rawDetails.scrollIntoView({block:'start'});
  });
  if (rawDetails?.open) render();
});
