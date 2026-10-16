(() => {
  // Indices address the original serialized Attempts array, including after UI filtering.
  // Findings are observed evidence, not defects or a claim about missing telemetry.
  const workerOrchestrationSignals = analysis => {
    const attempts = analysis?.Attempts || [];
    const losses = {attempts:analysis?.DroppedAttempts || 0,lifecycle:analysis?.DroppedLifecycleEvents || 0,identity:analysis?.DroppedIdentityBindings || 0};
    const result = {signals:[],omittedSignals:0,losses,incomplete:Object.values(losses).some(n=>n>0)};
    const retries = new Map(), groups = new Map();
    const validTime = n => Number.isSafeInteger(n) && n >= 0;
    const retrySignals = [], overlapSignals = [];
    attempts.forEach((a,index) => {
      if (!a.Scope || a.Ambiguous) return;
      if (a.Outcome === 'retry' && validTime(a.EndMS) && a.UUID && !a.IdentityAmbiguous) {
        const key=JSON.stringify([a.Scope,a.UUID]);
        let group=retries.get(key);
        if(!group) {group={scope:a.Scope,uuid:a.UUID,worker:a.Worker,count:0,attemptIndices:[]};retries.set(key,group);}
        group.count++;
        if(group.attemptIndices.length<16)group.attemptIndices.push(index);
      }
      if(a.Coverage!=='start_and_finish_observed' || !validTime(a.StartMS) || !validTime(a.EndMS) || a.EndMS<=a.StartMS || !a.Worker || a.Worker==='unknown') return;
      const key=JSON.stringify([a.Scope,a.Worker]);
      let group=groups.get(key);
      if(!group) {group={scope:a.Scope,worker:a.Worker,events:[]};groups.set(key,group);}
      group.events.push({time:a.StartMS,start:true,index},{time:a.EndMS,start:false,index});
    });
    for(const group of retries.values()) {
      if(group.count<2)continue;
      retrySignals.push({...group,kind:'repeated_retry',title:'Повторные результаты retry у задания',
        detail:`Наблюдалось ${group.count} завершений с retry в одной сессии.`,
        limitation:'Retry может быть штатной политикой восстановления. Это не доказательство непрерывного сбоя или отсутствия успешных попыток.'});
    }
    for(const group of groups.values()) {
      group.events.sort((a,b)=>a.time-b.time || Number(a.start)-Number(b.start) || a.index-b.index);
      const active=new Set();let peak=0,observedMS=null;
      for(const event of group.events) {
        if(event.start)active.add(event.index);else active.delete(event.index);
        if(active.size>peak){peak=active.size;observedMS=event.time;}
      }
      if(peak<2)continue;
      // A second linear pass captures bounded witnesses once, avoiding O(n^2)
      // copying when a large burst increases the peak at every start.
      const attemptIndices=[];
      for(const event of group.events) {
        if(event.start && attempts[event.index].StartMS<=observedMS && attempts[event.index].EndMS>observedMS && attemptIndices.length<16)attemptIndices.push(event.index);
      }
      overlapSignals.push({kind:'overlap',scope:group.scope,worker:group.worker,count:peak,observedMS,attemptIndices,
        title:'Перекрывающиеся выполнения одного класса',detail:`Одновременно наблюдалось ${peak} полных интервалов выполнения.`,
        limitation:'Параллельная работа может быть намеренной. Перекрытие не доказывает дублирование задания, нагрузку на CPU или причину проблем UI.'});
    }
    const strongestFirst = (a,b) => b.count-a.count;
    retrySignals.sort(strongestFirst);
    overlapSignals.sort(strongestFirst);
    // Reserve half the visible cards for each kind so a large retry cohort cannot
    // hide every overlap. Give unused places to the strongest remaining signals.
    const selectedRetries=retrySignals.slice(0,32),selectedOverlaps=overlapSignals.slice(0,32);
    const remaining=retrySignals.slice(32).concat(overlapSignals.slice(32)).sort(strongestFirst);
    result.signals=selectedRetries.concat(selectedOverlaps,remaining.slice(0,64-selectedRetries.length-selectedOverlaps.length)).sort(strongestFirst);
    result.omittedSignals=retrySignals.length+overlapSignals.length-result.signals.length;
    return result;
  };
  if(typeof module!=='undefined')module.exports={workerOrchestrationSignals};
  if(typeof window!=='undefined')window.jhWorkerOrchestrationSignals=workerOrchestrationSignals;
  if(typeof document==='undefined')return;
  const target=document.querySelector('[data-worker-insights]');
  const attemptsRoot=document.querySelector('[data-worker-attempts]');
  if(!target || !attemptsRoot)return;
  const analysis=attemptsRoot.jhAttemptsData || JSON.parse(
    attemptsRoot.querySelector('script[type="application/json"]').textContent);
  const result=workerOrchestrationSignals(analysis);
  const addText=(parent,tag,value)=>{const node=document.createElement(tag);node.textContent=value;parent.append(node);return node;};
  const heading=document.createElement('div');heading.className='thread-insights-head';
  addText(heading,'h3','Что требует проверки в WorkManager');
  addText(heading,'p',result.signals.length ? result.signals.length+' сигналов по наблюдённым попыткам' :
    'Проверяемых сигналов по попыткам пока нет');
  target.append(heading);
  if(!result.signals.length){
    const empty=addText(target,'p','Не выявлены повторные retry одного UUID или перекрытия полных попыток одного класса. Отсутствие сигнала не доказывает отсутствие проблем: часть событий и попыток могла не попасть в журнал.');
    empty.className='thread-insights-empty';
  }
  const list=document.createElement('div');list.className='thread-insights-list';
  result.signals.forEach(signal=>{
    const card=document.createElement('article');card.className='thread-insight-card';
    addText(card,'h4',signal.title);
    addText(card,'p',signal.detail);
    addText(card,'p','Класс: '+signal.worker+(signal.uuid ? ' · UUID: '+signal.uuid : '')+
      ' · сессия: '+signal.scope);
    if(signal.observedMS!=null)addText(card,'small','Пересечение зафиксировано около '+signal.observedMS+' мс elapsedRealtime.');
    const action=signal.kind==='repeated_retry' ?
      'Проверьте результат каждой попытки, причину retry и настройки backoff.' :
      'Проверьте, допустима ли параллельная работа этого класса и есть ли общий изменяемый ресурс.';
    addText(card,'p','Что проверить: '+action).className='thread-insight-action';
    addText(card,'small',signal.limitation).className='thread-insight-limit';
    const route=document.createElement('div');route.className='thread-insight-route';
    const button=addText(route,'button','Открыть подтверждающие попытки ('+signal.attemptIndices.length+')');
    button.type='button';
    button.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-worker-signal',
      {detail:{scope:signal.scope,count:signal.count,attemptIndices:signal.attemptIndices}})));
    card.append(route);list.append(card);
  });
  target.append(list);
  if(result.omittedSignals)addText(target,'p','Ещё '+result.omittedSignals+
    ' сигналов не показаны из-за ограничения списка. Сузьте исследование по исходным попыткам.').className='thread-insights-quality';
  if(result.incomplete)addText(target,'p','Охват неполный: пропущено карточек попыток '+result.losses.attempts+
    ', событий жизненного цикла '+result.losses.lifecycle+', сопоставлений UUID '+result.losses.identity+
    '. Количество сигналов - нижняя граница.').className='thread-insights-quality';
})();
