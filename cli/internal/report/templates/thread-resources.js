const threadResourceView=(items,scope,tid,query,page,window=null)=>{
  const needle=query.toLowerCase();
  const rows=(items || []).filter(row=>row.Scope===scope &&
    (!tid || row.CompletionTID===tid) &&
    (!window || row.EndMS>=window.start &&
      (row.StartMS==null ? row.EndMS<=window.end : row.StartMS<=window.end)) &&
    `${row.Kind} ${row.Source} ${row.Target || ''} ${row.Outcome} ${row.CompletionTID} ${row.OperationID || ''}`.toLowerCase().includes(needle));
  const pages=Math.max(1,Math.ceil(rows.length/50));
  page=Math.max(0,Math.min(page,pages-1));
  return {rows:rows.slice(page*50,(page+1)*50),total:rows.length,page,pages};
};
const threadResourceRelation=(row,window)=>{
  if(!window)return 'none';
  return window.operationID && row.OperationID===window.operationID?
    'same_application_operation':'temporal_overlap';
};
if(typeof module!=='undefined')module.exports={threadResourceView,threadResourceRelation};

if(typeof document!=='undefined')document.querySelectorAll('[data-thread-resources]').forEach(root=>{
  const section=root.closest('[data-thread-analysis]');
  const data=threadAnalysisData(section);
  const get=name=>root.querySelector('[data-resource-'+name+']');
  const process=get('process'),search=get('search'),body=get('rows');
  const scopes=[...new Map((data.Resources || []).map(row=>[row.Scope,row.Process])).entries()];
  scopes.forEach(([scope,name])=>{const option=document.createElement('option');option.value=scope;option.textContent=`${name} / ${scope}`;process.append(option);});
  let selectedTID='',selectedWindow=null,page=0;
  const draw=()=>{
    const view=threadResourceView(data.Resources,process.value,selectedTID,search.value,page,selectedWindow);
    page=view.page;body.replaceChildren();
    view.rows.forEach(row=>{
      const tr=document.createElement('tr');
      const relation=threadResourceRelation(row,selectedWindow);
      [row.Kind,row.Source || 'неизвестен',row.Target || 'не записана',
        row.StartMS==null?'неизвестно (длительность больше доступного времени)':row.StartMS+' мс (вычислено)',
        row.EndMS+' мс (событие завершения)',row.Outcome,row.CompletionTID,
        row.OperationID || 'нет',relation==='same_application_operation'?
          'тот же ID контекста, что на start/finish; владелец не доказан':
          relation==='temporal_overlap'?'совпадение времени':'—'].forEach(value=>{
        const td=document.createElement('td');td.textContent=value;tr.append(td);
      });
      body.append(tr);
    });
    get('page').textContent=`Страница ${page+1} из ${view.pages}; операций ${view.total}`;
    get('filter').textContent=selectedWindow?
      `Операции пересекались с наблюдавшимся интервалом попытки WorkManager ${selectedWindow.start}-${selectedWindow.end} мс в этой сессии. Совпадающий ID контекста записан на обоих концах попытки и у операции; это не доказывает принадлежности воркеру.`:
      selectedTID?`События завершения записаны на TID ${selectedTID} в этой сессии. TID может быть переиспользован; поколение потока не установлено для этих событий. Совпадение не доказывает владение ресурсом на всём интервале.`:'';
  };
  process.addEventListener('input',()=>{selectedTID='';selectedWindow=null;page=0;draw();});
  search.addEventListener('input',()=>{page=0;draw();});
  get('prev').addEventListener('click',()=>{page--;draw();});
  get('next').addEventListener('click',()=>{page++;draw();});
  document.addEventListener('jh-select-thread-resources',event=>{
    const detail=event.detail;if(!scopes.some(([scope])=>scope===detail.scope))return;
    process.value=detail.scope;selectedTID=detail.tid || '';selectedWindow=detail.startMS!=null && detail.endMS!=null?
      {start:detail.startMS,end:detail.endMS,operationID:detail.operationID || ''}:null;
    search.value='';page=0;draw();root.closest('details')?.setAttribute('open','');root.scrollIntoView({block:'start'});
  });
  draw();
});
