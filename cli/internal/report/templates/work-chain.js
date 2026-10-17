(() => {
  const workChainPage=(edges,query,scope,page)=>{
    const q=query.toLowerCase();
    const matching=edges.filter(e=>(!scope||e.Scope===scope)&&
      `${e.ParentUUID} ${e.ChildUUID} ${e.Process} ${e.Edge.CallerSource}`.toLowerCase().includes(q));
    const pages=Math.max(1,Math.ceil(matching.length/50));page=Math.max(0,Math.min(page,pages-1));
    return {rows:matching.slice(page*50,(page+1)*50),total:matching.length,page,pages};
  };
  const workChainRootPage=(roots,query,scope,page)=>{
    const q=query.toLowerCase();
    const matching=roots.filter(r=>(!scope||r.Scope===scope)&&
      `${r.UUID} ${r.Process} ${r.Root.UniqueName} ${r.Root.CallerSource}`.toLowerCase().includes(q));
    const pages=Math.max(1,Math.ceil(matching.length/50));page=Math.max(0,Math.min(page,pages-1));
    return {rows:matching.slice(page*50,(page+1)*50),total:matching.length,page,pages};
  };
  const workChainRootsForJob=(roots,scope,uuid)=>{
    const observations=[];let omitted=0;
    for(const root of roots){
      if(root.Scope!==scope||root.UUID!==uuid)continue;
      if(observations.length<4)observations.push(root);else omitted++;
    }
    return {observations,omitted};
  };
  const workChainForJob=(edges,scope,uuid)=>{
    const incoming=[],outgoing=[];
    let incomingOmitted=0,outgoingOmitted=0;
    for(const edge of edges){
      if(edge.Scope!==scope)continue;
      if(edge.ChildUUID===uuid){if(incoming.length<20)incoming.push(edge.ParentUUID);else incomingOmitted++;}
      if(edge.ParentUUID===uuid){if(outgoing.length<20)outgoing.push(edge.ChildUUID);else outgoingOmitted++;}
    }
    return {incoming,outgoing,incomingOmitted,outgoingOmitted};
  };
  if(typeof module!=='undefined')module.exports={workChainPage,workChainRootPage,workChainForJob,workChainRootsForJob};
  if(typeof document==='undefined')return;
  window.jhWorkChainForJob=workChainForJob;
  window.jhWorkChainRootsForJob=workChainRootsForJob;
  const root=document.querySelector('[data-work-chain]');if(!root)return;
  const data=JSON.parse(root.querySelector('script[type="application/json"]').textContent),edges=data.Edges||[],roots=data.Roots||[];
  const get=name=>root.querySelector('[data-chain-'+name+']');let page=0,rootPage=0;
  const draw=()=>{
    const view=workChainPage(edges,get('search').value,get('scope').value,page);page=view.page;
    const body=get('edges').querySelector('tbody');body.replaceChildren();
    view.rows.forEach(edge=>{
      const tr=document.createElement('tr');
      for(const uuid of [edge.ParentUUID,edge.ChildUUID]){
        const cell=document.createElement('td'),button=document.createElement('button');
        button.type='button';button.textContent=uuid;button.disabled=!document.querySelector('[data-work-registry]');
        button.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-job',{detail:{scope:edge.Scope,uuid}})));
        cell.append(button);tr.append(cell);
      }
      const fields=[edge.Edge.CallerSource+(edge.Edge.SourceTruncated?' (усечено)':''),
        `${edge.TimeMS} мс (${edge.TimeBasis==='observation_clock'?'календарный якорь':'время записи'})`,
        `${edge.Process}; ${edge.Scope}`];
      fields.forEach(value=>{const cell=document.createElement('td');cell.textContent=value;tr.append(cell);});
      body.append(tr);
    });
    get('page').textContent=`Страница ${page+1} из ${view.pages}; наблюдённых рёбер ${view.total}.`;
    const rootView=workChainRootPage(roots,get('search').value,get('scope').value,rootPage);rootPage=rootView.page;
    const rootBody=get('roots').querySelector('tbody');rootBody.replaceChildren();
    const policies=['обычная цепочка','KEEP','REPLACE','APPEND','APPEND_OR_REPLACE'];
    rootView.rows.forEach(value=>{
      const tr=document.createElement('tr'),cell=document.createElement('td'),button=document.createElement('button'),r=value.Root;
      button.type='button';button.textContent=value.UUID;button.disabled=!document.querySelector('[data-work-registry]');
      button.addEventListener('click',()=>document.dispatchEvent(new CustomEvent('jh-select-job',{detail:{scope:value.Scope,uuid:value.UUID}})));
      cell.append(button);tr.append(cell);
      [r.HasUnique?r.UniqueName+(r.NameTruncated?' (усечено)':''):'не задано',
        policies[r.Policy]||'неизвестно',r.CallerSource+(r.SourceTruncated?' (усечено)':''),
        `${value.TimeMS} мс (${value.TimeBasis==='observation_clock'?'календарный якорь':'время записи'})`,
        `${value.Process}; ${value.Scope}`].forEach(text=>{const td=document.createElement('td');td.textContent=text;tr.append(td);});
      rootBody.append(tr);
    });
    get('root-page').textContent=`Страница ${rootPage+1} из ${rootView.pages}; наблюдений построения ${rootView.total}.`;
  };
  ['search','scope'].forEach(name=>get(name).addEventListener('input',()=>{page=0;rootPage=0;draw();}));
  get('prev').addEventListener('click',()=>{page--;draw();});
  get('next').addEventListener('click',()=>{page++;draw();});
  get('root-prev').addEventListener('click',()=>{rootPage--;draw();});
  get('root-next').addEventListener('click',()=>{rootPage++;draw();});
  draw();
})();
