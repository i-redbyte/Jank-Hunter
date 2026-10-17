(() => {
  const workerSchedulePage = (rows, requestedPage) => {
    const pages=Math.max(1,Math.ceil(rows.length/50));
    const page=Math.max(0,Math.min(pages-1,requestedPage));
    return {page,pages,rows:rows.slice(page*50,(page+1)*50)};
  };
  const workerComparisonSelection=(rows,value)=>value === '' ? null : (rows[Number(value)] || null);
  if (typeof module !== 'undefined') module.exports={workerSchedulePage,workerComparisonSelection};
  if (typeof document === 'undefined') return;
  const root=document.querySelector('[data-worker-registry-compare]');if (!root) return;
  const rows=JSON.parse(root.querySelector('script[type="application/json"]').textContent) || [];
  const query=root.querySelector('input');const select=root.querySelector('select');
  const info=root.querySelector('[data-worker-compare-info]');
  const statesBody=root.querySelector('[data-worker-compare-states]');
  const schedulesBody=root.querySelector('[data-worker-compare-schedules]');
  let page=0;
  const cell=(row,value)=>{const td=document.createElement('td');td.textContent=value;row.append(td);};
  const draw=()=>{
    const selected=workerComparisonSelection(rows,select.value);statesBody.replaceChildren();schedulesBody.replaceChildren();
    if (!selected) {info.textContent='Наблюдения реестра отсутствуют или процесс не найден.';root.querySelector('[data-worker-compare-page]').textContent='';return;}
    info.textContent=selected.Note;
    ['Ожидает','Выполняется по наблюдению','Успешно','Ошибка','Заблокировано зависимостями','Отменено'].forEach((name,i)=>{
      const tr=document.createElement('tr');cell(tr,name);
      cell(tr,selected.BaselineKnown ? selected.BaselineStates[i] : 'нет данных');
      cell(tr,selected.CandidateKnown ? selected.CandidateStates[i] : 'нет данных');
      cell(tr,selected.Comparable ? selected.CandidateStates[i]-selected.BaselineStates[i] : 'не сравнивается');statesBody.append(tr);
    });
    const range=workerSchedulePage(selected.Schedules || [],page);page=range.page;
    range.rows.forEach(s=>{
      const tr=document.createElement('tr');cell(tr,s.Configuration);
      cell(tr,selected.BaselineKnown ? s.Baseline : 'нет данных');cell(tr,selected.CandidateKnown ? s.Candidate : 'нет данных');
      cell(tr,selected.Comparable ? s.Candidate-s.Baseline : 'не сравнивается');schedulesBody.append(tr);
    });
    root.querySelector('[data-worker-compare-page]').textContent=`Страница ${page+1} из ${range.pages}; конфигураций ${(selected.Schedules || []).length}.`;
  };
  const choose=()=>{
    select.replaceChildren();let matches=0;
    rows.forEach((r,i)=>{
      if (!(r.Process || '').toLowerCase().includes(query.value.toLowerCase()))return;
      if (matches++>=100)return;
      const option=document.createElement('option');option.value=i;option.textContent=r.Process || 'процесс неизвестен';select.append(option);
    });
    root.querySelector('[data-worker-compare-process-count]').textContent=`Найдено процессов: ${matches}; показаны первые 100, уточните поиск при необходимости.`;
    page=0;draw();
  };
  query.addEventListener('input',choose);select.addEventListener('change',()=>{page=0;draw();});
  root.querySelector('[data-worker-compare-prev]').addEventListener('click',()=>{page--;draw();});
  root.querySelector('[data-worker-compare-next]').addEventListener('click',()=>{page++;draw();});
  choose();
})();
