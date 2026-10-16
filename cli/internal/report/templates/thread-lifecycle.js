const threadLifecycleStageText = stage => ({1:'Первое наблюдение run в эпохе',2:'Thread.start вернулся',3:'Фабрика вернула Thread'}[stage] || 'неизвестно');
const threadLifecyclePage = (items, query, stage, page) => {
  const q = query.toLowerCase();
  const matches = items.filter(item => {
    const o = item.Observation;
    return (!stage || o.stage === stage) && `${item.Process} ${item.Scope} ${o.java_id || ''} ${o.target_tid || ''} ${o.name} ${o.source} ${o.factory || ''} ${item.Operation || ''} ${item.OperationID || ''}`.toLowerCase().includes(q);
  });
  const pages = Math.max(1, Math.ceil(matches.length / 50));
  page = Math.max(0, Math.min(page, pages-1));
  return {rows:matches.slice(page*50,(page+1)*50),total:matches.length,pages,page};
};
const initThreadLifecycle = (root, data) => {
  const section = root.querySelector('[data-thread-lifecycle]');
  if (!section) return;
  const get = name => section.querySelector('[data-thread-event-'+name+']');
  let page = 0;
  const draw = () => {
    const view = threadLifecyclePage(data.Lifecycle || [],get('search').value,Number(get('stage').value),page);
    page = view.page; const body=get('rows');body.replaceChildren();
    view.rows.forEach(item => {
      const o=item.Observation, row=document.createElement('tr');
      const context=item.ContextCaptured ? `Экран: ${item.Screen || 'неизвестно'}; операция: ${item.OperationID ? (item.Operation || 'имя неизвестно')+' #'+item.OperationID : 'нет активной'}; владелец: ${item.Owner || 'неизвестно'}` : 'Контекст не захвачен';
      [threadLifecycleStageText(o.stage),`${item.TimeMS} мс (${item.TimeBasis === 'observation_clock' ? 'монотонный якорь' : 'время записи'})`,item.Process,o.java_id || 'неизвестно',o.target_tid || 'неизвестно',item.RecordingTID || 'неизвестно',`${o.name}; ${o.source}; фабрика: ${o.factory || 'неизвестно'}`,context].forEach(value => {const cell=document.createElement('td');cell.textContent=value;row.appendChild(cell);});
      body.appendChild(row);
    });
    get('page').textContent=`Страница ${page+1} из ${view.pages}; найдено ${view.total}; отброшено CLI ${data.DroppedLifecycle || 0}. События не задают количество живых потоков.`;
  };
  [get('search'),get('stage')].forEach(input=>input.addEventListener('input',()=>{page=0;draw();}));
  get('prev').addEventListener('click',()=>{page--;draw();});get('next').addEventListener('click',()=>{page++;draw();});
  draw();
};
if(typeof module!=='undefined') module.exports={threadLifecycleStageText,threadLifecyclePage};
