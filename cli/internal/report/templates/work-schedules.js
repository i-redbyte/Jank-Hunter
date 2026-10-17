(function(){
'use strict';
const known=v=>typeof v==='number'&&Number.isSafeInteger(v)&&v>=0;
function scheduleDuration(v){
 if(!known(v))return 'неизвестно';
 if(v===0)return '0 мс';
 const units=[[86400000,'д'],[3600000,'ч'],[60000,'мин'],[1000,'с'],[1,'мс']];
 let left=v,parts=[];for(const [size,label]of units){const n=Math.floor(left/size);if(n){parts.push(n+' '+label);left%=size;if(parts.length===2)break;}}
 return parts.join(' ');
}
function scheduleTime(v,zone){
 if(!known(v)||v>8640000000000000)return 'время неизвестно';
 try{return new Intl.DateTimeFormat('ru-RU',{timeZone:zone||'UTC',year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hourCycle:'h23'}).format(new Date(v));}catch(_){return 'время неизвестно';}
}
function scheduleRequestedText(p){
 if(!p)return 'Создание и постановка запроса не наблюдались. Параметры приложения неизвестны.';
 const text=[];
 if(known(p.RequestedPeriodMS)){
  text.push('Запрошено повторение с периодом '+scheduleDuration(p.RequestedPeriodMS)+'.');
  if(known(p.RequestedFlexMS))text.push('Окно запуска: в пределах последних '+scheduleDuration(p.RequestedFlexMS)+' периода. Окно явно передано приложением (flex).');
  else text.push('Наблюдался конструктор без отдельного окна: WorkManager выбирает момент внутри периода.');
 }else text.push(p.Periodic?'Периодический запрос; значение периода и окно запуска не наблюдались.':'Одноразовый запрос; регулярное расписание этим запросом не задано.');
 text.push(known(p.RequestedInitialDelayMS)?'Задержка первого запуска, явно переданная приложением: '+scheduleDuration(p.RequestedInitialDelayMS)+'.':'Начальная задержка при создании не наблюдалась; значение по умолчанию не предполагается.');
 return text.join(' ');
}
function scheduleParameterExplanation(p){
 if(!p||![p.RequestedPeriodMS,p.RequestedFlexMS,p.RequestedInitialDelayMS].some(known))return 'Значения запроса неизвестны; допустимость параметров по этой записи не проверена.';
 const issues=[];
 if(known(p.RequestedPeriodMS)&&p.RequestedPeriodMS<900000)issues.push('Переданный период меньше минимального периода WorkManager 15 мин. WorkManager может увеличить его; сравните со снимком зарегистрированного задания.');
 if(known(p.RequestedFlexMS)&&p.RequestedFlexMS<300000)issues.push('Переданное окно меньше минимального окна WorkManager 5 мин; зарегистрированное значение может быть увеличено.');
 if(known(p.RequestedFlexMS)&&known(p.RequestedPeriodMS)&&p.RequestedFlexMS>p.RequestedPeriodMS)issues.push('Переданное окно длиннее периода; WorkManager может ограничить его размер.');
 if(issues.length)return issues.join(' ')+' Нормализация параметров сама по себе не доказывает сбой выполнения.';
 return 'Захваченные значения не выходят за проверяемые границы WorkManager. Пригодность срока для продукта неизвестна: допустимый срок обновления не задан, а точное время запуска Android не гарантирует.';
}
function scheduleRegistryText(row,zone){
 const j=row.Registered;if(!j)return 'Нет данных реестра для этого UUID в данной сессии и процессе.';
 const text=[];
 if(j.PeriodMS!=null)text.push('Зарегистрировано повторение с периодом '+scheduleDuration(j.PeriodMS)+'. Окно выбора запуска: '+scheduleDuration(j.FlexMS)+'.');
 else text.push('Зарегистрирована одноразовая задача.');
 text.push('Начальная задержка: '+scheduleDuration(j.InitialDelayMS)+'.');
 if(j.State===0&&known(j.EarliestEligibleUnixMS))text.push('По временным условиям запуск разрешён начиная с '+scheduleTime(j.EarliestEligibleUnixMS,zone)+'. Это не обещанное время выполнения.');
 const states=['ожидает выполнения','выполняется','завершена успешно','завершена с ошибкой','ожидает предшествующие задачи','отменена'];
 text.push('Состояние снимка: '+(states[j.State]||'неизвестно')+'.');
 text.push('Требования: '+scheduleRequirements(j)+'.');
 text.push('Снимок: '+(known(row.RegistryObservedUnixMS)?scheduleTime(row.RegistryObservedUnixMS,zone):scheduleDuration(row.RegistryObservedMS)+' по монотонным часам устройства')+'; возраст к концу записи: '+scheduleDuration(row.RegistryAgeMS)+'.');
 return text.join(' ');
}
function scheduleRequirements(j){
 const net={NOT_REQUIRED:'сеть не требуется',CONNECTED:'подключение к сети',UNMETERED:'сеть без тарификации трафика',NOT_ROAMING:'сеть без роуминга',METERED:'тарифицируемая сеть',TEMPORARILY_UNMETERED:'временно нетарифицируемая сеть'};
 const parts=[net[j.NetworkType]||'тип сети '+(j.NetworkType||'неизвестен')];
 for(const [bit,text]of [[1,'зарядка'],[2,'заряд батареи не низкий'],[4,'достаточно места'],[8,'устройство простаивает']])if(j.Constraints&bit)parts.push(text);
 if(j.Constraints&32)parts.push('особые требования к сети (детали не собраны)');
 if((j.Constraints&16)||(j.ContentTriggers||[]).length||j.OmittedContentTriggers)parts.push('изменения content URI');
 return parts.join(', ');
}
function scheduleAssessment(row){
 if(!row.Registered)return 'Проверить применение настроек нельзя: нет данных реестра.';
 if(row.RegistryBeforeRequest)return 'Снимок получен раньше запроса; применение новых параметров ещё не проверено.';
 if(!row.RegistryComplete)return 'Снимок неполный. Применение параметров и отсутствие заданий не подтверждены.';
 if(!known(row.RegistryAgeMS)||!known(row.RegistryIntervalMS)||row.RegistryAgeMS>row.RegistryIntervalMS)return 'Снимок устарел или его давность неизвестна. Текущее расписание не подтверждено.';
 if(row.ParametersDiffer)return 'Запрошенные и зарегистрированные параметры отличаются. Это наблюдаемое различие, причина пока не установлена: возможны сохранение прежнего задания, изменение параметров или нормализация WorkManager.';
 if(!row.Requested||![row.Requested.RequestedPeriodMS,row.Requested.RequestedFlexMS,row.Requested.RequestedInitialDelayMS].some(known))return 'Зарегистрированные параметры известны; значения запроса приложения отсутствуют, сравнение невозможно.';
 return 'Захваченные параметры не противоречат снимку. Соответствие срокам продукта не оценено: допустимый срок обновления не задан.';
}
function scheduleFactText(row){
 const c=row.Cadence||{},d=row.Duration||{};
 let text='Наблюдалось обычных запусков: '+(row.OrdinaryStarts||0)+'; запусков повторных попыток: '+(row.RetryStarts||0)+'. ';
 if(c.Count)text+='Между обычными началами одного UUID в последнем наблюдаемом поколении '+row.CadenceGeneration+': '+scheduleDuration(c.MinMS)+' - '+scheduleDuration(c.MaxMS)+'; медиана '+scheduleDuration(c.MedianMS)+' по '+c.Count+' промежуткам. ';
 else text+='В последнем наблюдаемом поколении недостаточно обычных запусков для оценки интервалов. ';
 if(d.Count)text+='Длительность завершённых попыток: медиана '+scheduleDuration(d.MedianMS)+', 95-й процентиль '+scheduleDuration(d.P95MS)+' ('+d.Count+' наблюдений). ';
 if(row.AmbiguousStarts)text+='Неоднозначных наблюдений, исключённых из сравнения: '+row.AmbiguousStarts+'. ';
 return text+'Ненаблюдавшийся запуск не считается пропущенным.';
}
function scheduleFindingAtThreshold(f,limit){
 if(f.Kind==='replacement_churn')return f;
 const bucket=(f.Proximity||[]).find(b=>b.LimitMS===limit);
 return bucket&&bucket.Count?{...f,Count:bucket.Count,MinGapMS:bucket.MinGapMS,MaxGapMS:bucket.MaxGapMS}:null;
}
function scheduleGroups(rows){
 const groups=new Map();
 function clock(r){try{return BigInt(r.Requested&&r.Requested.ObservedElapsedNanos||0);}catch(_){return 0n;}}
 for(const r of rows){const key=JSON.stringify([r.Scope,r.Process,r.Name]);let g=groups.get(key);
  if(!g){g={Name:r.Name,Scope:r.Scope,Process:r.Process,Worker:r.Worker,Rows:[],Latest:r};groups.set(key,g);}
  g.Rows.push(r);if(clock(r)>clock(g.Latest))g.Latest=r;
 }
 return [...groups.values()];
}
function schedulePage(rows,query,scope,page){
 const q=String(query||'').trim().toLowerCase();
 const selected=rows.filter(r=>(!scope||r.Scope===scope)&&(!q||[r.Name,r.Worker,r.UUID,r.Process,r.Requested&&r.Requested.CallerSource,...(r.Rows||[]).map(v=>[v.UUID,v.Worker,v.Requested&&v.Requested.CallerSource].join(' '))].join(' ').toLowerCase().includes(q)));
 const last=Math.max(0,Math.ceil(selected.length/20)-1),p=Math.max(0,Math.min(page||0,last));
 return {rows:selected.slice(p*20,p*20+20),total:selected.length,page:p,last};
}
function scheduleTimeline(rows,forecast){
 const points=[];
 rows.forEach((r,index)=>{
  (r.Attempts||[]).forEach(a=>{if(known(a.StartUnixMS))points.push({index,at:a.StartUnixMS,end:known(a.EndUnixMS)&&a.EndUnixMS>=a.StartUnixMS?a.EndUnixMS:a.StartUnixMS,kind:a.RunAttempt>0?'retry':'run',name:r.Name});});
  const j=r.Registered;
  if(j&&j.State===0&&known(j.EarliestEligibleUnixMS)){
   points.push({index,at:j.EarliestEligibleUnixMS,end:j.EarliestEligibleUnixMS,kind:'eligible',name:r.Name});
   if(forecast&&known(j.PeriodMS)&&j.PeriodMS>0)for(let n=1;n<=3;n++){
    const at=j.EarliestEligibleUnixMS+n*j.PeriodMS;
    if(known(at))points.push({index,at,end:at,kind:'estimate',name:r.Name});
   }
  }
 });
 return points;
}
if(typeof module!=='undefined')module.exports={scheduleDuration,scheduleTime,scheduleRequestedText,scheduleRegistryText,scheduleAssessment,scheduleFactText,scheduleParameterExplanation,scheduleFindingAtThreshold,scheduleGroups,schedulePage,scheduleTimeline};
if(typeof document==='undefined')return;
const host=document.querySelector('[data-work-schedules]');if(!host)return;
let data;try{data=JSON.parse(host.querySelector('script[type="application/json"]').textContent);}catch(_){return;}
const rows=data.Rows||[], groups=scheduleGroups(rows), search=host.querySelector('[data-schedule-search]'), scopes=host.querySelector('[data-schedule-scope]'), zone=host.querySelector('[data-schedule-zone]'), near=host.querySelector('[data-schedule-near]'), forecast=host.querySelector('[data-schedule-forecast]');
let page=0;
function el(tag,text,parent){const n=document.createElement(tag);if(text!==undefined)n.textContent=text;if(parent)parent.appendChild(n);return n;}
const localZone=Intl.DateTimeFormat().resolvedOptions().timeZone;
for(const z of [...new Set([localZone,'Europe/Moscow','UTC'])]){const o=el('option',z,zone);o.value=z;}
for(const scope of [...new Set(rows.map(r=>r.Scope))]){const item=rows.find(r=>r.Scope===scope);const o=el('option',(item.Process||'процесс неизвестен')+' / '+scope.slice(-12),scopes);o.value=scope;}
function paragraph(card,title,text){el('h4',title,card);el('p',text,card);}
function cardFor(row,parent){
 const detail=el('details',undefined,parent);el('summary','Подробный разбор: '+row.Name+' ('+row.UUID.slice(0,8)+')',detail);
 paragraph(detail,'Что запросило приложение',scheduleRequestedText(row.Requested));
 paragraph(detail,'Допустимость и пригодность настройки',scheduleParameterExplanation(row.Requested));
 paragraph(detail,'Откуда взялись данные',row.Requested?'Зафиксированы параметры публичных вызовов при работе приложения. Место постановки: '+(row.Requested.CallerSource&&row.Requested.CallerSource!=='unknown'?row.Requested.CallerSource:'неизвестно')+(row.Requested.SourceTruncated?' (имя усечено)':'')+'. Связь значения с Omicron или другой настройкой автоматически не предполагается.':'Создание запроса не наблюдалось. Наличие класса в каталоге не доказывает его постановку.');
 paragraph(detail,'Что зарегистрировано',scheduleRegistryText(row,zone.value));
 paragraph(detail,'Что произошло на устройстве',scheduleFactText(row));
 for(const gen of row.GenerationCadences||[]){const c=gen.Cadence;if(c.Count)paragraph(detail,'Интервалы поколения '+gen.Generation,'Наблюдаемых промежутков: '+c.Count+'; от '+scheduleDuration(c.MinMS)+' до '+scheduleDuration(c.MaxMS)+'; медиана '+scheduleDuration(c.MedianMS)+'. Значения других поколений в эту оценку не входят.');}
 paragraph(detail,'Как оценивать результат',scheduleAssessment(row));
 const history=el('details',undefined,detail);el('summary','История запросов и снимков',history);
 for(const v of row.Requests||[]){paragraph(history,'Запрос: '+(v.TimeBasis==='observation_clock'?scheduleTime(v.TimeMS,zone.value):scheduleDuration(v.TimeMS)+' по часам журнала'),scheduleRequestedText(v.Submission)+' Место вызова: '+(v.Submission.CallerSource&&v.Submission.CallerSource!=='unknown'?v.Submission.CallerSource:'неизвестно')+'.');}
 for(const v of row.RegistryHistory||[]){paragraph(history,'Снимок: '+(known(v.ObservedUnixMS)?scheduleTime(v.ObservedUnixMS,zone.value):scheduleDuration(v.ObservedMS)+' по монотонным часам устройства'),'Поколение '+v.Job.Generation+'; период '+scheduleDuration(v.Job.PeriodMS)+'; окно '+scheduleDuration(v.Job.FlexMS)+'; начальная задержка '+scheduleDuration(v.Job.InitialDelayMS)+'. Требования: '+scheduleRequirements(v.Job)+'. '+(v.Complete?'Пакеты снимка получены полностью.':'Снимок неполный.'));}
 const attempts=el('details',undefined,detail);el('summary','Наблюдаемые начала и завершения',attempts);
 for(const v of row.Attempts||[]){el('p',(v.RunAttempt>0?'Повторная попытка':'Обычная попытка')+'; поколение '+v.Generation+'; начало '+(known(v.StartUnixMS)?scheduleTime(v.StartUnixMS,zone.value):known(v.StartMS)?scheduleDuration(v.StartMS)+' по часам сессии':'не наблюдалось')+'; завершение '+(known(v.EndUnixMS)?scheduleTime(v.EndUnixMS,zone.value):known(v.EndMS)?scheduleDuration(v.EndMS)+' по часам сессии':'не наблюдалось')+'; результат '+({success:'успех',failure:'ошибка',retry:'запрошен повтор',cancelled:'отмена',stopped:'остановка'}[v.Outcome]||'неизвестен')+'.',attempts);}
 const technical=el('details',undefined,detail);el('summary','Идентификаторы и технические параметры',technical);
 el('pre',JSON.stringify({UUID:row.UUID,Scope:row.Scope,Process:row.Process,Worker:row.Worker,Requested:row.Requested,Registered:row.Registered},null,2),technical);
}
function renderFindings(){
 const target=host.querySelector('[data-schedule-findings]');target.replaceChildren();
 const findings=(data.Findings||[]).map(f=>scheduleFindingAtThreshold(f,Number(near.value))).filter(f=>f&&(!scopes.value||f.Scope===scopes.value)&&(!search.value.trim()||[f.Name,f.OtherName].join(' ').toLowerCase().includes(search.value.trim().toLowerCase())));
 if(!findings.length){el('p','В доступных данных подходящие наблюдения близких запусков или повторных постановок не найдены. Это не подтверждает их отсутствие за пределами записи.',target);return;}
 for(const f of findings.slice(0,50)){
  const d=el('details',undefined,target);
  if(f.Kind==='replacement_churn'){
   el('summary',f.Name+': постановок с заменой '+f.Count+' за '+scheduleDuration(f.MaxGapMS),d);
   el('p','Зафиксированы разные UUID с одним полным unique name и политикой замены. Проверьте повторную постановку из нескольких мест. Количество вызовов не доказывает число регистраций, отмен или выполнений.',d);
  }else{
   el('summary',f.Name+' + '+f.OtherName+': '+(f.Kind==='adjacent_starts'?'близкие фактические начала':'близкие ранние допустимые отметки'),d);
   el('p','Наблюдений соседних отметок: '+f.Count+'; расстояние '+scheduleDuration(f.MinGapMS)+' - '+scheduleDuration(f.MaxGapMS)+'. '+(f.Kind==='adjacent_starts'?'Это наблюдаемая близость обычных запусков, без повторных попыток.':'Это сравнение временных условий из снимков, совместное выполнение не подтверждено. Не учитывает фактическую доступность сети и ограничения Android.'),d);
   el('p',f.SameRequirements?'В доступных снимках совпадают захваченные требования. Проверьте возможность согласовать расписания; отдельно оцените независимость ошибок, повторов и отмены перед объединением логики.':'Совпадение требований не установлено. Близость времени сама по себе не обосновывает объединение задач.',d);
  }
  el('small','Процесс: '+(f.Process||'неизвестен')+'; сессия '+f.Scope.slice(-12)+'. Анализируются соседние отметки; это не полный перебор всех сочетаний.',d);
 }
 if(findings.length>50)el('p','Показаны первые 50 наблюдений. Уточните поиск.',target);
}
function renderTimeline(visible){
 const target=host.querySelector('[data-schedule-timeline]');target.replaceChildren();
 const points=scheduleTimeline(visible,forecast.checked);
 if(!points.length){el('p','Для календарной шкалы нет начал выполнения или ранних допустимых отметок с известным календарным временем. Монотонные часы разных сессий не совмещаются.',target);return;}
 const min=Math.min(...points.map(p=>p.at)),max=Math.max(...points.map(p=>p.end)),span=Math.max(1,max-min);
 const ns='http://www.w3.org/2000/svg',svg=document.createElementNS(ns,'svg');svg.setAttribute('viewBox','0 0 1200 '+(visible.length*36+65));svg.setAttribute('role','img');svg.setAttribute('aria-label','Наблюдаемые выполнения и ранние допустимые отметки');svg.style.minWidth='900px';svg.style.width='100%';target.appendChild(svg);
 function item(tag,attrs,text){const n=document.createElementNS(ns,tag);for(const [k,v]of Object.entries(attrs))n.setAttribute(k,String(v));if(text!==undefined)n.textContent=text;svg.appendChild(n);return n;}
 const x=t=>290+(t-min)/span*880;
 visible.forEach((r,i)=>{item('text',{x:0,y:40+i*36,fill:'currentColor','font-size':12},r.Name.length>34?r.Name.slice(0,32)+'…':r.Name);item('line',{x1:290,x2:1170,y1:36+i*36,y2:36+i*36,stroke:'currentColor','stroke-opacity':.18});});
 for(const p of points){const y=36+p.index*36,color={run:'#62c8a0',retry:'#f6bd60',eligible:'#8cbcff',estimate:'#b1a3d4'}[p.kind];let n;
  if(p.kind==='run'||p.kind==='retry')n=item('rect',{x:x(p.at),y:y-6,width:Math.max(4,x(p.end)-x(p.at)),height:12,fill:color});
  else n=item('line',{x1:x(p.at),x2:x(p.at),y1:y-10,y2:y+10,stroke:color,'stroke-width':3,'stroke-dasharray':p.kind==='estimate'?'3 3':''});
  const title=document.createElementNS(ns,'title');title.textContent=p.name+': '+({run:'наблюдаемая попытка',retry:'повторная попытка',eligible:'ранняя допустимая отметка из снимка',estimate:'условное продолжение периода'}[p.kind])+' - '+scheduleTime(p.at,zone.value);n.appendChild(title);
 }
 el('p',scheduleTime(min,zone.value)+' - '+scheduleTime(max,zone.value)+'. Часовой пояс: '+zone.value+'. Зелёный - выполнение, жёлтый - повтор, синий - ранняя допустимая отметка; пунктир - условная оценка.',target);
 if(forecast.checked)el('p','Пунктир продолжает период из последнего снимка на три цикла. Это арифметическая иллюстрация при неизменных настройках, не прогноз решения Android. Новые завершения, ограничения и изменения задания могут сдвинуть отметки.',target);
}
function render(){
 const view=schedulePage(groups,search.value,scopes.value,page);page=view.page;
 const tbody=host.querySelector('tbody');tbody.replaceChildren();
 for(const group of view.rows){const row=group.Latest,tr=el('tr',undefined,tbody);const name=el('td',undefined,tr);el('strong',group.Name,name);el('p',row.Worker||'Класс не установлен',name);el('small',(group.Process||'Процесс неизвестен')+'; сессия '+group.Scope.slice(-12)+'; UUID в группе: '+group.Rows.length,name);
  el('td',(group.Rows.length>1?'Последний захваченный запрос. ':'')+scheduleRequestedText(row.Requested),tr);
  if(group.Rows.length===1){el('td',scheduleRegistryText(row,zone.value),tr);el('td',scheduleFactText(row),tr);}
  else {
   const registered=group.Rows.filter(v=>v.Registered).sort((a,b)=>b.RegistryObservedMS-a.RegistryObservedMS);
   el('td',registered.length?'Снимки есть для '+registered.length+' UUID. Последний из них: '+scheduleRegistryText(registered[0],zone.value):'Нет данных реестра для UUID этой группы.',tr);
   el('td','Всего захваченных обычных запусков: '+group.Rows.reduce((n,v)=>n+v.OrdinaryStarts,0)+'; повторных попыток: '+group.Rows.reduce((n,v)=>n+v.RetryStarts,0)+'. Интервалы разных UUID не смешиваются; отдельные оценки доступны в разборе.',tr);
  }
  const assessment=el('td',group.Rows.length===1?scheduleAssessment(row):'Несколько UUID с одним именем в одной сессии. Применение настроек проверяется для каждого UUID отдельно; одинаковое имя не доказывает принятие последнего запроса.',tr);
  for(const member of group.Rows)cardFor(member,assessment);
 }
 host.querySelector('[data-schedule-page]').textContent='Страница '+(page+1)+' из '+(view.last+1)+'; групп заданий '+view.total+'.';
 host.querySelector('[data-schedule-prev]').disabled=page===0;host.querySelector('[data-schedule-next]').disabled=page===view.last;
 const unavailable=data.RegistryUnavailable||0;
 const registeredCount=rows.filter(r=>r.Registered).length;
 const reasonLabels={'1':'источник отсутствует или WorkManager ещё не готов','2':'ошибка получения данных','3':'истёк срок ожидания ответа','4':'неподдерживаемый источник'};
 const reasons=Object.entries(data.RegistryUnavailableReasons||{}).map(([reason,count])=>(reasonLabels[reason]||'причина неизвестна')+' ('+count+' снимков)').join('; ');
 host.querySelector('[data-schedule-status]').textContent='Групп заданий: '+groups.length+'. '+(!registeredCount?(data.EmptyCompleteSnapshots?'Есть полные снимки без наблюдаемых заданий. Это состояние на момент снимка; принятие запросов приложения им не подтверждается. ':'Действующее расписание неизвестно: снимков с заданиями нет. '):'Снимки WorkManager есть для '+registeredCount+' UUID. ')+(reasons?'Причины недоступности: '+reasons+'. ':'')+(data.CollectionKnownLost?'В общей записи есть потери событий; оценка частоты может быть неполной.':'');

 host.querySelector('[data-schedule-coverage]').textContent='Групп заданий: '+groups.length+'; UUID в сводке: '+rows.length+'. Снимков с недоступным источником: '+unavailable+'; неполных снимков: '+(data.IncompleteSnapshots||0)+'. Пропущено исходных наблюдений: '+(data.DroppedEvidence||0)+'. Ограничения сводки: не включено наблюдений новых заданий '+(data.OmittedRows||0)+', элементов истории '+(data.OmittedHistory||0)+', попыток '+(data.OmittedAttempts||0)+', рекомендаций '+(data.OmittedFindings||0)+'. '+(data.HistoryOmitted?'Исходная история сокращена. ':'')+'Качество общей записи: '+({high:'высокое',medium:'среднее',low:'низкое',unknown:'неизвестно'}[data.CollectionLevel]||data.CollectionLevel||'неизвестно')+'; известных потерь событий по всей записи: '+(data.CollectionKnownLost||0)+'. Эти потери не распределяются автоматически на конкретных воркеров. Короткая запись и потери событий ограничивают выводы о частоте; отсутствие данных не означает отсутствие работы.';
 renderTimeline(view.rows.flatMap(g=>g.Rows).slice(0,20));renderFindings();
}
function reveal(target){
 for(let d=target&&target.closest('details');d;d=d.parentElement&&d.parentElement.closest('details'))d.open=true;
}
document.addEventListener('click',event=>{
 const link=event.target.closest&&event.target.closest('.async-local-nav a');
 if(link&&link.hash)reveal(document.getElementById(link.hash.slice(1)));
});
document.addEventListener('jh-select-job',()=>reveal(document.getElementById('work-registry')));
search.addEventListener('input',()=>{page=0;render();});scopes.addEventListener('change',()=>{page=0;render();});zone.addEventListener('change',render);near.addEventListener('change',render);forecast.addEventListener('change',render);
host.querySelector('[data-schedule-prev]').addEventListener('click',()=>{page--;render();});host.querySelector('[data-schedule-next]').addEventListener('click',()=>{page++;render();});render();
})();
