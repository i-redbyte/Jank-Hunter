const coroutineSourceOrigin = typeof module !== 'undefined' ? require('./thread-snapshot.js').sourceOrigin : sourceOrigin;
const coroutineOwnerLabel = category => ({app:'источник корутины: код приложения',system:'источник корутины: системный код',library:'источник корутины: код библиотеки',collector:'источник корутины: Jank Hunter',unknown:'источник корутины не классифицирован'})[category] || 'источник корутины не классифицирован';
const coroutineSegmentView = (items,scope,javaID,query,page,tid='',origin='',roots=[]) => {
  const needle=query.toLowerCase();
  const endpoint=(id,nativeID)=>(!javaID || id===javaID) && (!tid || nativeID===tid);
  const rows=(items || []).filter(s=>s.Scope===scope &&
    (endpoint(s.EntryJavaThreadID,s.EntryTID) || endpoint(s.ExitJavaThreadID,s.ExitTID)) &&
    (!origin || coroutineSourceOrigin(s.Owner,roots)===origin) &&
    `${s.Owner} ${s.Token} ${s.EntryJavaThreadID} ${s.ExitJavaThreadID} ${s.EntryTID || ''} ${s.ExitTID || ''}`.toLowerCase().includes(needle));
  const pages=Math.max(1,Math.ceil(rows.length/50));page=Math.max(0,Math.min(page,pages-1));
  return {rows:rows.slice(page*50,(page+1)*50),total:rows.length,page,pages};
};
const coroutineIntervalText = segment => segment.State==='running_observed' ?
  `${segment.StartedElapsedMS} → наблюдалась до ${segment.ObservedThroughElapsedMS} мс; конец неизвестен` :
  `${segment.StartedElapsedMS}-${segment.EndedElapsedMS} мс`;
const coroutineThreadIndex = processes => {
  const index=new Map();
  (processes || []).forEach((p,process)=>{
    (p.Snapshots || []).forEach((snapshot,point)=>{
      if(snapshot.TimeBasis!=='observation_clock')return;
      const elapsed=snapshot.Snapshot?.elapsed_ms;
      if(!Number.isSafeInteger(elapsed))return;
      for(const row of snapshot.Snapshot.threads || []){
        if(typeof row.java?.id!=='string')continue;
        const key=p.Scope+'|'+row.java.id;
        const observation={process,point,tid:String(row.tid),elapsed};
        for(const identity of [key,key+'|tid|'+observation.tid]){
          if(!index.has(identity))index.set(identity,[]);
          index.get(identity).push(observation);
        }
      }
    });
  });
  for(const rows of index.values())rows.sort((a,b)=>a.elapsed-b.elapsed);
  return index;
};
const coroutineThreadCandidate = (processes,segment,index=coroutineThreadIndex(processes)) => {
  const key=segment.Scope+'|'+segment.EntryJavaThreadID;
  const rows=index.get(segment.EntryTID ? key+'|tid|'+segment.EntryTID : key) || [];
  if(!rows.length)return null;
  let low=0,high=rows.length;
  while(low<high){const mid=(low+high)>>1;if(rows[mid].elapsed<segment.StartedElapsedMS)low=mid+1;else high=mid;}
  const choices=[rows[low-1],rows[low]].filter(Boolean);
  const best=choices.reduce((a,b)=>!a || Math.abs(b.elapsed-segment.StartedElapsedMS)<Math.abs(a.elapsed-segment.StartedElapsedMS)?b:a,null);
  return {process:best.process,point:best.point,tid:best.tid,distance:Math.abs(best.elapsed-segment.StartedElapsedMS)};
};
if(typeof module!=='undefined')module.exports={coroutineSegmentView,coroutineIntervalText,coroutineThreadIndex,coroutineThreadCandidate};

if(typeof document!=='undefined')document.querySelectorAll('[data-thread-coroutines]').forEach(root=>{
  const section=root.closest('[data-thread-analysis]');
  const data=threadAnalysisData(section);
  const get=name=>root.querySelector('[data-coroutine-'+name+']');
  const process=get('process'),search=get('search'),body=get('rows'),origin=get('origin'),appRoots=section.querySelector('[data-thread-app-roots]');
  const states={suspended:'приостановлена после сегмента',completed:'завершён',failure:'исключение',cancelled:'отменён',running_observed:'выполнялась при сборе; исход неизвестен'};
  const threadIndex=coroutineThreadIndex(data.Processes);
  const scopes=[...new Map((data.CoroutineSegments || []).map(s=>[s.Scope,s.Process])).entries()];
  scopes.forEach(([scope,name])=>{const option=document.createElement('option');option.value=scope;option.textContent=`${name} / ${scope}`;process.append(option);});
  let page=0,selectedJavaID='',selectedTID='';
  const draw=()=>{
    const view=coroutineSegmentView(data.CoroutineSegments,process.value,selectedJavaID,search.value,page,selectedTID,origin.value,sourceRoots(appRoots.value));
    page=view.page;body.replaceChildren();
    view.rows.forEach(segment=>{
      const tr=document.createElement('tr');
      [segment.Owner + ' · ' + coroutineOwnerLabel(sourceOrigin(segment.Owner,sourceRoots(appRoots.value))),`${segment.Token} / ${segment.Generation}`,
        coroutineIntervalText(segment),
        `${segment.EntryJavaThreadID} / ${segment.ExitJavaThreadID || 'неизвестен'}`,
        `${segment.EntryTID || 'неизвестен'} / ${segment.ExitTID || 'неизвестен'}`,
        states[segment.State] || 'неизвестно'].forEach(value=>{const td=document.createElement('td');td.textContent=value;tr.append(td);});
      const cell=document.createElement('td'),button=document.createElement('button');button.type='button';
      const candidate=coroutineThreadCandidate(data.Processes,segment,threadIndex);
      button.textContent=candidate?`Ближайший снимок TID ${candidate.tid} (разница ${candidate.distance} мс)`:'TID не наблюдался';button.disabled=!candidate;
      button.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-java-thread',
        {detail:{scope:segment.Scope,javaID:segment.EntryJavaThreadID,at:segment.StartedElapsedMS,...candidate}})));
      cell.append(button);tr.append(cell);body.append(tr);
    });
    get('page').textContent=`Страница ${page+1} из ${view.pages}; записей ${view.total}`;
    get('filter').textContent=selectedJavaID?`Показаны сегменты Java Thread ${selectedJavaID}${selectedTID?' на TID '+selectedTID:''} в выбранной сессии по совпадающей паре идентификаторов одного конца сегмента. Это интервалы выполнения, а не владение потоком между ними.`:
      selectedTID?`Показаны сегменты с TID ${selectedTID}; без Java ID поколение потока не подтверждено.`:'';
  };
  process.addEventListener('input',()=>{page=0;selectedJavaID='';selectedTID='';draw();});
  search.addEventListener('input',()=>{page=0;draw();});
  origin.addEventListener('input',()=>{page=0;draw();});
  appRoots.addEventListener('input',()=>{page=0;draw();});
  get('prev').addEventListener('click',()=>{page--;draw();});get('next').addEventListener('click',()=>{page++;draw();});
  document.addEventListener('jh-select-coroutines',event=>{
    const detail=event.detail;if(!scopes.some(([scope])=>scope===detail.scope))return;
    process.value=detail.scope;selectedJavaID=detail.javaID || '';selectedTID=detail.tid || '';search.value='';origin.value='';page=0;draw();root.closest('details')?.setAttribute('open','');root.scrollIntoView({block:'start'});
  });
  draw();
});
