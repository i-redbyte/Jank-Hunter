(() => {
  const attemptJobObservationText = value => {
    if (!value) return 'Последнее состояние задания неизвестно: нет однозначного наблюдения UUID в этой сессии.';
    const states=['ожидает','выполняется по последнему наблюдению','успешно','ошибка','заблокировано зависимостями','отменено'];
    const freshness={fresh:'в пределах интервала опроса',stale:'устаревшее',unknown:'свежесть неизвестна'};
    return `Задание ${states[value.State] || 'с неизвестным состоянием'}; наблюдение ${value.ObservedElapsedMS} мс elapsedRealtime; возраст ${value.AgeAtCaptureMS == null ? 'неизвестен' : value.AgeAtCaptureMS+' мс'}; ${freshness[value.Freshness] || freshness.unknown}. ${value.AbsentFromLaterCompleteSnapshot ? 'В более позднем полном снимке отсутствует; это не доказывает завершение.' : ''} Состояние конкретной попытки неизвестно по этому источнику: счётчики WorkInfo и WorkerParameters различаются; периодические выполнения могут переиспользовать номер.`;
  };
  const attemptJobProgressText = value => {
    if (!value) return 'Метаданные progress задания неизвестны: нет однозначного наблюдения WorkInfo этого UUID в этой сессии.';
    const fields = (value.Progress || []).map(field => `${field.Key || 'ключ неизвестен'} (${field.Type || 'тип неизвестен'}, ${field.PayloadBytes == null ? 'размер неизвестен' : field.PayloadBytes + ' байт'})`);
    return `Последнее наблюдение WorkInfo в ${value.ObservedElapsedMS} мс elapsedRealtime: ${fields.length ? fields.join('; ') : 'ключи progress не наблюдались'}; пропущено ключей: ${value.OmittedProgressKeys || 0}. Это метаданные задания, которые могут относиться к другой попытке. Значения Data не записаны.`;
  };
  const attemptDirectProgressText = value => {
    const requests=value?.ProgressRequests || [];
    if (!requests.length) return `Прямые запросы progress этой попытки не наблюдались; отсутствие событий не доказывает отсутствие вызовов. Всего пропущено вызовов: ${value?.ProgressOmittedUpdates || 0}.`;
    const rows=requests.map(request=>`${request.ObservedMS} мс: ${(request.Fields || []).map(field=>`${field.Key || 'ключ неизвестен'} (${field.Type || 'тип неизвестен'}, ${field.PayloadBytes == null ? 'размер неизвестен' : field.PayloadBytes+' байт'})`).join('; ') || 'ключи не наблюдались'}; пропущено ключей: ${request.OmittedKeys || 0}; усечено полей: ${request.TruncatedFields || 0}`);
    return `${rows.join('\n')}\nВсего пропущено вызовов: ${value.ProgressOmittedUpdates || 0}. Это вызовы setProgressAsync базового воркера; запрос не подтверждает сохранение WorkManager. Значения Data не записаны.`;
  };
  const attemptForegroundText = value => {
    if (value.ForegroundEnteredMS == null) return 'Подтверждённый переход в foreground не наблюдался. Это не доказывает, что его не было: нужны инструментированные методы базового Worker.';
    if (value.ForegroundBoundaryMS == null) return `Переход подтверждён в ${value.ForegroundEnteredMS} мс elapsedRealtime; конец наблюдения неизвестен.`;
    return `Переход подтверждён в ${value.ForegroundEnteredMS} мс; граница наблюдения попытки ${value.ForegroundBoundaryMS} мс; между ними ${Math.max(0,value.ForegroundBoundaryMS-value.ForegroundEnteredMS)} мс. Фактический выход из foreground/остановка службы неизвестны.`;
  };
  const attemptExecutionSegmentsText = value => {
    const segments = value.ExecutionSegments || [];
    const millis = value => {
      if (value == null) return 'неизвестно';
      try {
        if (typeof value === 'number' && !Number.isSafeInteger(value)) return 'неизвестно';
        const ns = BigInt(value);
        if (ns < 0n) return 'неизвестно';
        return `${ns/1000000n}.${(ns%1000000n/1000n).toString().padStart(3,'0')}`;
      } catch { return 'неизвестно'; }
    };
    const rows = segments.slice(0,16).map(s => `${s.Source || 'метод неизвестен'}; Linux TID ${s.TargetTID || 'неизвестен'}; elapsed ${millis(s.DurationNS)} мс; CPU ${millis(s.CPUNS)} мс`);
    return `${rows.join('\n') || 'Исполнительные сегменты не наблюдались.'}\nПропуски: пропущено сегментов: ${value.ExecutionSegmentsOmitted || 0}; пропущено событий: ${value.ExecutionEventsOmitted || 0}. `+
      'Связь с попыткой подтверждается причинными ID. Покрытие частичное; сегменты могут быть вложенными, их CPU нельзя складывать. Время приостановки корутины не включено; неизвестный CPU не равен нулю.';
  };
  const attemptUsesCalendar = a => (a.StartMS != null || a.EndMS != null) && (a.StartMS == null || a.StartUnixMS != null) && (a.EndMS == null || a.EndUnixMS != null);
  const attemptPlotInterval = a => {
    const calendar=attemptUsesCalendar(a),start=calendar ? a.StartUnixMS : a.StartMS,end=calendar ? a.EndUnixMS : a.EndMS;
    if (start != null && end != null) return {start,end:Math.max(start,end),kind:'interval'};
    const point = start == null ? (end == null ? a.FailureObservedMS : end) : start;
    return {start:point,end:point,kind:start == null ? (end == null ? 'failure' : 'finish') : 'start'};
  };
  const filterAttempts = (items, query, process, outcome, from, to, scope='', uuid='') => items.filter(a => {
    const interval = attemptPlotInterval(a);
    return (!scope || a.Scope === scope) && (!uuid || a.UUID === uuid) &&
      `${a.Worker || ''} ${a.InstanceID || ''} ${a.UUID || ''} ${a.Operation || ''} ${a.FailureClass || ''}`.toLowerCase().includes(query.toLowerCase()) &&
      (!process || a.Process === process) && (!outcome || a.Outcome === outcome) &&
      (from == null || interval.end >= from) && (to == null || interval.start <= to);
  });
  const attemptsAtThreadObservation = (items, observation) => {
    if (!observation.scope || !observation.tid || observation.time == null || observation.radius == null || observation.radius < 0) return [];
    return items.filter(a => a.Scope === observation.scope && (
      (a.StartThreadID === observation.tid && a.StartUnixMS != null && Math.abs(a.StartUnixMS-observation.time) <= observation.radius) ||
      (a.EndThreadID === observation.tid && a.EndUnixMS != null && Math.abs(a.EndUnixMS-observation.time) <= observation.radius)));
  };
  if (typeof module !== 'undefined') module.exports = {attemptExecutionSegmentsText,attemptUsesCalendar,attemptJobObservationText,attemptJobProgressText,attemptDirectProgressText,attemptForegroundText,attemptPlotInterval,filterAttempts,attemptsAtThreadObservation};
  if (typeof document === 'undefined') return;
  const root = document.querySelector('[data-worker-attempts]');
  if (!root) return;
  const threadHref=root.closest('#workers')?.dataset.threadHref || '';
  const selectThreadView=(anchor,type,detail)=>{
    if(threadHref)window.jhNavigateSelection(threadHref+'#'+anchor,type,detail);
    else document.dispatchEvent(new CustomEvent(type,{detail}));
  };
  const data = JSON.parse(root.querySelector('script[type="application/json"]').textContent);
  root.jhAttemptsData = data;
  const items = data.Attempts || [];
  const query = root.querySelector('[data-attempt-search]');
  const process = root.querySelector('[data-attempt-process]');
  const outcome = root.querySelector('[data-attempt-outcome]');
  const from = root.querySelector('[data-attempt-from]');
  const to = root.querySelector('[data-attempt-to]');
  const body = root.querySelector('tbody');
  const card = root.querySelector('[data-attempt-card]');
  const canvas = root.querySelector('canvas');
  const outcomes = {success:'успех',failure:'ошибка',retry:'retry',cancelled:'отмена',stopped:'остановка',unknown:'неизвестен'};
  const coverage = {start_and_finish_observed:'start и finish наблюдались',missing_start:'начало не наблюдалось',missing_finish:'завершение неизвестно',failure_only:'только исключение; start/finish неизвестны'};
  for (const name of [...new Set(items.map(a => a.Process || ''))].filter(Boolean).sort()) {
    const option = document.createElement('option'); option.value = name; option.textContent = name; process.append(option);
  }
  let page = 0, threadObservation = null, linkedScope = '', linkedUUID = '', linkedAttemptIndices = null;
  const number = input => input.value === '' ? null : Number(input.value);
  const timestamp = value => value == null ? 'неизвестно' : `${value} мс`;
  const jobButton = root.querySelector('[data-attempt-job]');
  const resourceButton = root.querySelector('[data-attempt-resources]');
  let selectedAttempt;
  jobButton.addEventListener('click', () => {
    if (selectedAttempt?.UUID) document.dispatchEvent(new CustomEvent('jh-select-job',{detail:{scope:selectedAttempt.Scope,uuid:selectedAttempt.UUID}}));
  });
  resourceButton.addEventListener('click', () => {
    if (!selectedAttempt || selectedAttempt.StartMS == null || selectedAttempt.EndMS == null) return;
    selectThreadView('thread-resources','jh-select-thread-resources',{
      scope:selectedAttempt.Scope,startMS:selectedAttempt.StartMS,endMS:selectedAttempt.EndMS,
      operationID:selectedAttempt.OperationID || ''});
  });
  const threadButtons = ['start','end'].map(stage => {
    const button = root.querySelector('[data-attempt-thread-' + stage + ']');
    button.addEventListener('click', () => {
      if (!selectedAttempt) return;
      const a = selectedAttempt;
      selectThreadView('thread-snapshots','jh-select-thread',{scope:a.Scope,tid:stage === 'start' ? a.StartThreadID : a.EndThreadID,time:stage === 'start' ? a.StartUnixMS : a.EndUnixMS});
    });
    return button;
  });
  const details = a => {
    selectedAttempt = a;
    threadButtons[0].disabled = !a.StartThreadID || a.StartUnixMS == null || (!threadHref && !document.querySelector('[data-thread-analysis]'));
    threadButtons[1].disabled = !a.EndThreadID || a.EndUnixMS == null || (!threadHref && !document.querySelector('[data-thread-analysis]'));
    jobButton.disabled = !a.UUID || !document.querySelector('[data-work-registry]');
    resourceButton.disabled = a.StartMS == null || a.EndMS == null ||
      !(root.closest('#workers')?.dataset.threadResourcesAvailable==='true' || document.querySelector('[data-thread-resources]'));
    card.textContent = JSON.stringify({
      'Последнее наблюдение задания':attemptJobObservationText(a.LastJobObservation),
      'Метаданные progress последнего WorkInfo':attemptJobProgressText(a.LastJobObservation),
      'Прямые запросы progress попытки':attemptDirectProgressText(a),
      'Foreground попытки':attemptForegroundText(a),
      'Измеренные исполнительные сегменты':attemptExecutionSegmentsText(a),
      'WorkInfo: номер / поколение':a.LastJobObservation ? [a.LastJobObservation.WorkInfoRunAttempt,a.LastJobObservation.Generation] : 'неизвестно',
      'UUID':a.UUID || 'неизвестен','Неоднозначное сопоставление UUID':a.IdentityAmbiguous,
      'Класс':a.Worker,'Процесс':a.Process,'Область сессии':a.Scope,'Идентификатор runtime':a.InstanceID,
      'Календарный start / finish для связи со снимками':[a.StartUnixMS,a.EndUnixMS],
      'Попытка / поколение':[a.RunAttempt,a.Generation], 'Начало':a.StartMS,'Завершение':a.EndMS,
      'Измеренная длительность finish, мс':a.DurationMS,'Результат':outcomes[a.Outcome],
      'Охват':coverage[a.Coverage], 'Неоднозначное сопоставление повторных start':a.Ambiguous,
      'Причина остановки':a.StopReason,'TID при start':a.StartThreadID || 'неизвестен','TID при finish':a.EndThreadID || 'неизвестен',
      'Исключение':a.FailureClass || 'не наблюдалось',
      'Время исключения, мс elapsedRealtime':a.FailureObservedMS,
      'Ограниченный стек исключения':a.FailureStack || 'не наблюдался',
      'Стек обрезан':a.FailureStackTruncated,'Число записей об исключении':a.FailureCount || 0,
      'Экран / операция / владелец':[a.Screen,a.Operation,a.Owner],
      'Одинаковый ID контекста на start/finish':a.OperationID || 'не подтверждён',
      'Контекст на start и finish противоречит друг другу':!!a.OperationContextAmbiguous,
      'Ограничение потоков':'Это потоки записи отдельных событий; выполнение могло менять поток.',
      'Отсутствующий finish':'Не доказывает продолжение или завершение; процесс мог умереть, событие могло быть потеряно.',
    },null,2);
  };
  const plot = rows => {
    canvas.width = 1000; canvas.height = Math.max(60,rows.length * 20 + 35);
    const ctx = canvas.getContext('2d'); if (!ctx) return;
    const clocks=new Set(rows.map(a=>attemptUsesCalendar(a) ? 'calendar' : 'elapsed:'+a.Scope));
    if(clocks.size>1) {ctx.fillStyle='#718096';ctx.fillText('Нет общей временной шкалы: часть записей без календарного якоря. Используйте фильтры.',20,20);return;}
    const intervals = rows.map(attemptPlotInterval);
    if (!intervals.length) return;
    const left = Math.min(...intervals.map(i => i.start)), right = Math.max(...intervals.map(i => i.end));
    const x = value => 20 + (value-left) / Math.max(1,right-left)*960;
    ctx.fillStyle='#718096'; ctx.font='12px sans-serif'; ctx.fillText(`${left} мс (${rows.every(attemptUsesCalendar) ? 'календарный якорь' : 'elapsedRealtime'})`,20,14); ctx.fillText(`${right} мс`,880,14);
    intervals.forEach((i,index) => {
      const y = 30 + index*20;
      ctx.fillStyle=rows[index].Outcome === 'failure' ? '#c2410c' : '#0f766e';
      if (i.kind === 'interval') ctx.fillRect(x(i.start),y,Math.max(2,x(i.end)-x(i.start)),8);
      else {ctx.beginPath();ctx.arc(x(i.start),y+4,4,0,Math.PI*2);ctx.fill();}
    });
  };
  const draw = () => {
    const selected = linkedAttemptIndices ? linkedAttemptIndices.map(index => items[index]).filter(Boolean) :
      threadObservation ? attemptsAtThreadObservation(items,threadObservation) : items;
    const rows = filterAttempts(selected,query.value,process.value,outcome.value,number(from),number(to),linkedScope,linkedUUID);
    const pages = Math.max(1,Math.ceil(rows.length/50)); page=Math.max(0,Math.min(page,pages-1));
    const visible = rows.slice(page*50,(page+1)*50);
    body.replaceChildren();
    visible.forEach(a => {
      const tr=document.createElement('tr'); const td=document.createElement('td'); const button=document.createElement('button');
      button.type='button';button.textContent=a.Worker || 'класс неизвестен';button.addEventListener('click',()=>details(a));td.append(button);tr.append(td);
      [a.Process || 'неизвестен',`${a.RunAttempt} / ${a.Generation}`,timestamp(attemptUsesCalendar(a) ? a.StartUnixMS : a.StartMS),timestamp(attemptUsesCalendar(a) ? a.EndUnixMS : a.EndMS),timestamp(a.DurationMS),outcomes[a.Outcome],a.FailureClass || '—',coverage[a.Coverage]+(a.Ambiguous ? '; неоднозначно' : '')].forEach(text=>{const cell=document.createElement('td');cell.textContent=text;tr.append(cell);});
      body.append(tr);
    });
    root.querySelector('[data-attempt-page]').textContent=`Страница ${page+1} из ${pages}; найдено ${rows.length}. Пропущено карточек: ${data.DroppedAttempts || 0}; lifecycle-событий: ${data.DroppedLifecycleEvents || 0}; сопоставлений UUID: ${data.DroppedIdentityBindings || 0}; progress-событий: ${data.DroppedProgressEvents || 0}; progress без однозначной попытки: ${data.UnattributedProgressEvents || 0}; foreground без однозначной попытки: ${data.UnattributedForegroundEvents || 0}. При пропусках сводные значения отражают только сохранённую часть.`;
    plot(visible);
  };
  [query,process,outcome,from,to].forEach(control=>control.addEventListener('input',()=>{page=0;threadObservation=null;linkedScope='';linkedUUID='';linkedAttemptIndices=null;root.querySelector('[data-attempt-thread-link]').textContent='';draw();}));
  root.querySelector('[data-attempt-prev]').addEventListener('click',()=>{page--;draw();});
  root.querySelector('[data-attempt-next]').addEventListener('click',()=>{page++;draw();});
  document.addEventListener('jh-select-attempts', event => {
    threadObservation = null; linkedAttemptIndices = null; linkedScope = event.detail.scope || ''; linkedUUID = event.detail.uuid || '';
    root.querySelector('[data-attempt-thread-link]').textContent = linkedScope ?
      `Попытки UUID ${linkedUUID} только в выбранной сессии ${linkedScope}. Измените фильтр, чтобы сбросить связь.` : '';
    query.value = event.detail.uuid; process.value = ''; outcome.value = ''; from.value = ''; to.value = ''; page=0;
    draw(); root.scrollIntoView({block:'start'});
  });
  document.addEventListener('jh-select-thread-attempts', event => {
    threadObservation = event.detail; linkedAttemptIndices = null; linkedScope = ''; linkedUUID = ''; query.value = ''; process.value = ''; outcome.value = ''; from.value = ''; to.value = ''; page = 0;
    root.querySelector('[data-attempt-thread-link]').textContent = 'Показаны start/finish на TID ' + threadObservation.tid + ' в той же сессии, в пределах ' + threadObservation.radius + ' мс от снимка. Это временные кандидаты: поколение TID в событии неизвестно. Пустая выборка не доказывает отсутствие выполнения. Измените фильтр, чтобы сбросить связь.';
    draw(); root.scrollIntoView({block:'start'});
  });
  document.addEventListener('jh-select-worker-signal', event => {
    const detail = event.detail || {};
    linkedAttemptIndices = [...new Set((detail.attemptIndices || []).filter(index =>
      Number.isInteger(index) && index >= 0 && index < items.length && items[index].Scope === detail.scope))];
    threadObservation = null; linkedScope = detail.scope || ''; linkedUUID = '';
    query.value = ''; process.value = ''; outcome.value = ''; from.value = ''; to.value = ''; page = 0;
    root.querySelector('[data-attempt-thread-link]').textContent =
      'Показаны ' + linkedAttemptIndices.length + ' исходных попыток, подтверждающих сигнал; всего наблюдалось ' +
      (detail.count || linkedAttemptIndices.length) + '. Если свидетельств больше 16, карточка показывает первые 16. Измените фильтр, чтобы сбросить связь.';
    draw(); root.scrollIntoView({block:'start'});
  });
  draw();
})();
