(() => {
  const workSubmissionSourceText=p=>p.CallerSource && p.CallerSource!=='unknown' ? p.CallerSource+(p.SourceTruncated?' (усечено)':'')+' · источник до R8' : 'источник вызова неизвестен';
  const workSubmissionHintsText=p=>{
    const backoff={1:'линейный',2:'экспоненциальный'}[p.BackoffPolicy];
    const quota={1:'выполнить как обычную работу',2:'отбросить запрос'}[p.QuotaPolicy];
    return `явный backoff: ${backoff ? backoff+', запрос '+p.RequestedBackoffMS+' мс' : 'не наблюдался'}; expedited/quota: ${quota || 'не наблюдался'}`;
  };
  const policies=['обычная постановка','KEEP','REPLACE','APPEND','APPEND_OR_REPLACE','UPDATE','CANCEL_AND_REENQUEUE'];
  const workSubmissionPage=(items,query,policy,scope,page)=>{
    const q=query.toLowerCase();
    const rows=items.filter(v=>(!scope || v.Scope===scope) && (!policy || v.Submission.Policy===policy) && `${v.UUID} ${v.Submission.UniqueName} ${v.Process} ${v.Operation || ''} ${v.Submission.CallerSource || ''}`.toLowerCase().includes(q));
    const pages=Math.max(1,Math.ceil(rows.length/50));page=Math.max(0,Math.min(page,pages-1));
    return {rows:rows.slice(page*50,(page+1)*50),total:rows.length,page,pages};
  };
  if(typeof module!=='undefined')module.exports={workSubmissionPage,workSubmissionSourceText,workSubmissionHintsText};
  if(typeof document==='undefined')return;
  const root=document.querySelector('[data-work-submissions]');if(!root)return;
  const data=JSON.parse(root.querySelector('script[type="application/json"]').textContent),items=data.Observations || [];
  const get=name=>root.querySelector('[data-submission-'+name+']');let page=0;
  const draw=()=>{
    const view=workSubmissionPage(items,get('search').value,Number(get('policy').value),get('scope').value,page);page=view.page;
    const body=root.querySelector('tbody');body.replaceChildren();
    view.rows.forEach(v=>{
      const row=document.createElement('tr'),cell=document.createElement('td'),button=document.createElement('button'),p=v.Submission;
      button.type='button';button.textContent=v.UUID;button.disabled=!document.querySelector('[data-work-registry]');button.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-job',{detail:{scope:v.Scope,uuid:v.UUID}})));cell.appendChild(button);row.appendChild(cell);
      const context=v.ContextCaptured ? `экран: ${v.Screen || 'неизвестно'}; операция: ${v.Operation || 'неизвестно'} #${v.OperationID || 'нет'}; ${v.Process}` : `контекст неизвестен; ${v.Process}`;
      [`${v.TimeMS} мс (${v.TimeBasis==='observation_clock'?'календарный якорь':'elapsedRealtime записи'})`,p.HasUnique ? p.UniqueName+(p.NameTruncated?' (усечено, полное имя неизвестно)':'') : 'unique name не передан',policies[p.Policy] || 'неизвестно',p.Periodic?'периодический запрос':'одноразовый запрос',workSubmissionSourceText(p)+'; '+workSubmissionHintsText(p),context+'; сессия: '+v.Scope].forEach(value=>{const td=document.createElement('td');td.textContent=value;row.appendChild(td);});body.appendChild(row);
    });
    get('page').textContent=`Страница ${page+1} из ${view.pages}; запросов в вызовах ${view.total}. UUID ведёт к наблюдению реестра той же сессии, если оно есть.`;
  };
  ['search','policy','scope'].forEach(name=>get(name).addEventListener('input',()=>{page=0;draw();}));
  get('prev').addEventListener('click',()=>{page--;draw();});get('next').addEventListener('click',()=>{page++;draw();});
  if(document.readyState==='loading')document.addEventListener('DOMContentLoaded',draw,{once:true});
  draw();
})();
