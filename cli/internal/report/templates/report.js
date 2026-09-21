
(() => {
  const tableScope = (root) => root && root.querySelectorAll ? root : document;

  const markScrollableTables = (root = document) => {
    tableScope(root).querySelectorAll('.table-scroll').forEach((wrapper) => {
      wrapper.classList.toggle('is-scrollable', wrapper.scrollWidth > wrapper.clientWidth + 4);
    });
  };
  let tableMeasureFrame = 0;
  const scheduleTableMeasure = () => {
    if (tableMeasureFrame) return;
    tableMeasureFrame = requestAnimationFrame(() => {
      tableMeasureFrame = 0;
      markScrollableTables();
    });
  };

  const wrapTables = (root = document) => {
    tableScope(root).querySelectorAll('table').forEach((table) => {
      if (table.closest('.table-scroll')) return;
      const wrapper = document.createElement('div');
      wrapper.className = 'table-scroll';
      table.parentNode.insertBefore(wrapper, table);
      wrapper.appendChild(table);
    });
    scheduleTableMeasure();
  };

  const leakFieldForHeader = (label) => {
    const normalized = (label || '').trim().toLocaleLowerCase('ru').replaceAll('ё', 'е');
    if (normalized.includes('размер')) return 'size';
    if (normalized.includes('риск') || normalized.includes('статус')) return 'priority';
    if (normalized.includes('объект')) return 'subject';
    if (normalized.includes('тип')) return 'kind';
    if (normalized.includes('держател')) return 'owner';
    if (normalized.includes('контекст')) return 'context';
    if (normalized.includes('количество') || normalized.includes('изменение')) return 'change';
    if (normalized.includes('оценк')) return 'score';
    if (normalized.includes('цепоч') || normalized.includes('путь')) return 'path';
    if (normalized.includes('влияни')) return 'impact';
    if (normalized.includes('провер')) return 'action';
    if (normalized.includes('доказател') || normalized.includes('объяснен')) return 'evidence';
    return 'detail';
  };

  const prepareLeakCardTable = (table) => {
    table.classList.add('leak-card-table');
    table.closest('.table-scroll')?.classList.add('leak-card-scroll');
    const headerRow = table.querySelector('thead tr') || Array.from(table.rows).find((row) => row.querySelector('th'));
    if (!headerRow) return;
    headerRow.classList.add('leak-card-header-row');
    const headers = Array.from(headerRow.querySelectorAll('th'));
    const sortableHeaders = headers.filter((header) => header.querySelector('[data-code-sort]'));
    headerRow.classList.toggle('leak-card-static-header', sortableHeaders.length === 0);
    sortableHeaders.forEach((header) => header.classList.add('leak-card-sort-control'));
    table.querySelectorAll('tr[data-code-problem-row]').forEach((row) => {
      row.classList.add('leak-card-row');
      if (!row.getAttribute('aria-label')) {
        row.setAttribute('aria-label', `Сигнал удержания: ${row.dataset.class || 'объект'}`);
      }
      Array.from(row.cells).forEach((cell, index) => {
        const label = headers[index]?.textContent?.trim().replace(/\s+/g, ' ') || `Сведения ${index + 1}`;
        cell.dataset.label = label;
        cell.dataset.leakField = leakFieldForHeader(label);
      });
    });
  };

  const prepareLeakCardTables = (root = document) => {
    const tables = new Set();
    if (root.matches?.('table.leak-table')) tables.add(root);
    root.closest?.('table.leak-table') && tables.add(root.closest('table.leak-table'));
    tableScope(root).querySelectorAll('table.leak-table').forEach((table) => tables.add(table));
    tables.forEach(prepareLeakCardTable);
  };

  wrapTables();
  prepareLeakCardTables();


  const visibleTableScope = (node) => !node.closest('details:not([open]), [hidden]');
  const initialTableRoots = new Set();
  document.querySelectorAll('.table-scroll').forEach((wrapper) => {
    if (!visibleTableScope(wrapper)) return;
    initialTableRoots.add(wrapper);
  });

  const runIdle = (callback) => {
    if ('requestIdleCallback' in window) {
      window.requestIdleCallback(callback, { timeout: 700 });
      return;
    }
    window.setTimeout(() => {
      const idleStartedAt = performance.now();
      callback({
        timeRemaining: () => Math.max(0, 8 - (performance.now() - idleStartedAt)),
      });
    }, 0);
  };

  const forEachChunk = (nodes, chunkSize, visit, done) => {
    let index = 0;
    const step = (deadline) => {
      const start = Date.now();
      while (
        index < nodes.length &&
        (index % chunkSize !== 0 ||
          deadline.timeRemaining() > 2 ||
          Date.now() - start < 12)
      ) {
        visit(nodes[index]);
        index += 1;
      }
      if (index < nodes.length) {
        runIdle(step);
      } else if (done) {
        done();
      }
    };
    runIdle(step);
  };

  const normalizeTooltipText = (text) =>
    (text || '')
      .replace(/[ \t\r\f\v]+/g, ' ')
      .replace(/ *\n */g, '\n')
      .replace(/\n{3,}/g, '\n\n')
      .trim();

  const readableTooltipText = (node) => {
    if (!node) return '';
    const clone = node.cloneNode(true);
    clone.querySelectorAll('script, style, .cell-toggle').forEach((element) => element.remove());
    clone.querySelectorAll('br').forEach((element) => element.replaceWith('\n'));
    clone.querySelectorAll('h1, h2, h3, h4, h5, h6, p, div, summary, li, tr').forEach((element) => {
      element.insertAdjacentText('afterend', '\n');
    });
    clone.querySelectorAll('strong, em, small, code, span, td, th, button').forEach((element) => {
      element.insertAdjacentText('afterend', ' ');
    });
    return normalizeTooltipText(clone.textContent);
  };

  const enhanceLongCell = (cell) => {
    if (cell.dataset.cellEnhanced === 'true') return;
    if (cell.querySelector('table, canvas, svg, input, select, textarea, details, .cell-toggle')) return;
    const text = cell.textContent.trim().replace(/\s+/g, ' ');
    const overflows = cell.scrollWidth > cell.clientWidth + 4 || cell.scrollHeight > 180;
    if (text.length < 120 && !overflows) return;
    const clip = document.createElement('div');
    clip.className = 'table-cell-clip';
    while (cell.firstChild) {
      clip.appendChild(cell.firstChild);
    }
    const toggle = document.createElement('button');
    toggle.type = 'button';
    toggle.className = 'cell-toggle';
    toggle.textContent = 'развернуть';
    toggle.setAttribute('aria-expanded', 'false');
    toggle.addEventListener('click', () => {
      const expanded = !clip.classList.contains('is-expanded');
      clip.classList.toggle('is-expanded', expanded);
      toggle.textContent = expanded ? 'свернуть' : 'развернуть';
      toggle.setAttribute('aria-expanded', String(expanded));
      scheduleTableMeasure();
    });
    cell.append(clip, toggle);
    cell.dataset.cellEnhanced = 'true';
  };

  const enhanceTableRow = (row) => {
    row.querySelectorAll('td').forEach(enhanceLongCell);
  };

  let rowObserver = null;
  if ('IntersectionObserver' in window) {
    rowObserver = new IntersectionObserver((entries) => {
      entries.forEach((entry) => {
        if (!entry.isIntersecting) return;
        rowObserver.unobserve(entry.target);
        enhanceTableRow(entry.target);
      });
      scheduleTableMeasure();
    }, { rootMargin: '600px 0px' });
  }

  const enhanceLongCells = (root = document) => {
    prepareLeakCardTables(root);
    const rows = Array.from(tableScope(root).querySelectorAll('.table-scroll tr'));
    if (rowObserver) {
      rows.forEach((row) => {
        if (row.dataset.cellRowObserved === 'true') return;
        row.dataset.cellRowObserved = 'true';
        rowObserver.observe(row);
      });
      return;
    }
    forEachChunk(rows, 60, enhanceTableRow, scheduleTableMeasure);
  };

  const deferredLoads = new WeakMap();
  const nextDeferredChunk = (tbody) => tbody.querySelector('script[data-table-chunk]');
  const updateDeferredLoader = (tbody, loadedCount) => {
    const loader = tbody.querySelector('[data-deferred-loader]');
    if (!loader) return;
    const loaded = Number(loader.dataset.deferredLoaded || 0) + loadedCount;
    const total = Number(loader.dataset.deferredTotal || loaded);
    const remaining = Math.max(0, total - loaded);
    loader.dataset.deferredLoaded = String(loaded);
    if (!remaining) {
      loader.remove();
      return;
    }
    const nextCount = Math.min(
      remaining,
      Number(nextDeferredChunk(tbody)?.dataset.tableChunkSize || remaining),
    );
    const button = loader.querySelector('[data-load-more-rows]');
    const remainingLabel = loader.querySelector('[data-deferred-remaining]');
    if (button) button.textContent = 'Показать ещё ' + nextCount;
    if (remainingLabel) remainingLabel.textContent = 'Осталось строк: ' + remaining;
  };

  const materializeDeferredScript = (tbody, script, announce = true) => {
    if (!script) return [];
    const markup = JSON.parse(script.textContent || '""');
    const stagingTable = document.createElement('table');
    const stagingBody = stagingTable.createTBody();
    stagingBody.innerHTML = markup;
    const rows = Array.from(stagingBody.rows);
    const fragment = document.createDocumentFragment();
    rows.forEach((row) => fragment.appendChild(row));
    script.replaceWith(fragment);
    updateDeferredLoader(tbody, rows.length);
    enhanceLongCells(tbody);
    scheduleTableMeasure();
    if (announce && rows.length) {
      tbody.dispatchEvent(new CustomEvent('report:rows-added', { bubbles: true, detail: { rows } }));
    }
    return rows;
  };

  const materializeDeferredChunk = (tbody, announce = true) =>
    materializeDeferredScript(tbody, nextDeferredChunk(tbody), announce);

  const materializeAllDeferredRows = (tbody) => {
    const active = deferredLoads.get(tbody);
    if (active) return active;
    const button = tbody.querySelector('[data-load-more-rows]');
    if (button) {
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      button.textContent = 'Загрузка…';
    }
    const promise = new Promise((resolve, reject) => {
      const step = () => {
        const started = performance.now();
        try {
          while (nextDeferredChunk(tbody) && performance.now() - started < 8) {
            materializeDeferredChunk(tbody, true);
          }
        } catch (error) {
          reject(error);
          return;
        }
        if (nextDeferredChunk(tbody)) {
          requestAnimationFrame(step);
          return;
        }
        resolve();
      };
      requestAnimationFrame(step);
    });
    deferredLoads.set(tbody, promise);
    const releaseLoad = () => {
      if (deferredLoads.get(tbody) === promise) deferredLoads.delete(tbody);
    };
    void promise.then(releaseLoad, () => {
      releaseLoad();
      const retryButton = tbody.querySelector('[data-load-more-rows]');
      if (retryButton) {
        retryButton.disabled = false;
        retryButton.removeAttribute('aria-busy');
        updateDeferredLoader(tbody, 0);
      }
    });
    return promise;
  };

  const revealDeferredSearchEntry = async (entry) => {
    const tbody = entry.deferredBody;
    const script = entry.deferredScript;
    const searchID = entry.searchID;
    if (!tbody || !searchID) return entry.node;
    const selector = `[data-report-search-id="${searchID}"]`;
    let node = tbody.querySelector(selector);
    if (!node && script?.isConnected) {
      materializeDeferredScript(tbody, script);
      await new Promise((resolve) => requestAnimationFrame(resolve));
      node = tbody.querySelector(selector);
    }
    return node;
  };

  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-load-more-rows]');
    if (!button || button.disabled) return;
    const tbody = button.closest('tbody');
    if (!tbody) return;
    try {
      materializeDeferredChunk(tbody);
    } catch (error) {
      button.disabled = true;
      button.textContent = 'Не удалось загрузить строки';
      console.error(error);
    }
  });

  initialTableRoots.forEach((root) => {
    enhanceLongCells(root);
  });

  document.querySelectorAll('details').forEach((details) => {
    details.addEventListener('toggle', () => {
      if (!details.open) return;
      wrapTables(details);
      enhanceLongCells(details);
      scheduleTableMeasure();
    });
  });

  const reportTermHelp = new Map([
    ['4xx', 'HTTP 4xx - ответ о проблеме в запросе или правах клиента.'],
    ['5xx', 'HTTP 5xx - ответ о внутренней ошибке сервера.'],
    ['abi', 'ABI описывает машинную архитектуру, для которой собран нативный код приложения.'],
    ['anr', 'ANR - ситуация, когда Android считает приложение не отвечающим пользователю.'],
    ['api', 'API level - версия набора системных возможностей Android.'],
    ['asm', 'ASM анализирует байткод во время сборки и добавляет точки сбора данных Jank Hunter.'],
    ['cpu', 'CPU - процессор устройства. Высокая загрузка может задерживать главный поток и фоновые задачи.'],
    ['dao', 'DAO - слой методов, через который приложение читает и изменяет данные в базе.'],
    ['di', 'DI - внедрение зависимостей: объект получает готовые зависимости вместо создания их внутри себя.'],
    ['dns', 'DNS преобразует имя сервера в сетевой адрес. Задержка здесь происходит до соединения.'],
    ['fps', 'FPS - число отрисованных кадров в секунду. Смотрите его вместе с долей медленных кадров.'],
    ['gc', 'GC - сборщик мусора, который освобождает объекты, больше не достижимые из работающего приложения.'],
    ['hprof', 'HPROF - дамп Java/Kotlin-кучи. Он позволяет проверить путь ссылок от корня GC до объекта.'],
    ['http', 'HTTP - протокол сетевых запросов приложения.'],
    ['ipc', 'IPC - обмен данными между процессами Android.'],
    ['jank', 'Jank - заметная пользователю задержка отрисовки, когда кадр не уложился в доступное время.'],
    ['mad', 'MAD - медиана абсолютных отклонений. Показывает устойчивый разброс значений.'],
    ['p50', 'p50 - медиана: половина измерений не превышает это значение.'],
    ['p90', 'p90 - значение, которое не превысили 90% измерений.'],
    ['p95', 'p95 - значение, которое не превысили 95% измерений.'],
    ['p99', 'p99 - значение, которое не превысили 99% измерений; на малой выборке оно нестабильно.'],
    ['pss', 'PSS - память процесса с пропорционально учтёнными общими страницами.'],
    ['r8', 'R8 оптимизирует и переименовывает классы и методы Android-приложения. Mapping возвращает исходные имена.'],
    ['ram', 'RAM - оперативная память устройства.'],
    ['room', 'Room - Android-библиотека доступа к SQLite через DAO и сгенерированный код.'],
    ['rps', 'RPS - число запросов в секунду.'],
    ['rss', 'RSS - все физические страницы памяти процесса, включая общие страницы целиком.'],
    ['sql', 'SQL - язык запросов к базе данных.'],
    ['sqlite', 'SQLite - встроенная база данных Android.'],
    ['ttfb', 'TTFB - от начала первой отправки заголовков запроса до чтения первого байта ответа из транспорта, до HTTP-парсера. Включает отправку тела; интервалы могут пересекаться.'],
    ['uid', 'UID - системный идентификатор приложения Android.'],
    ['vpn', 'VPN направляет сетевой трафик через виртуальное соединение и может менять задержки.'],
    ['websocket', 'WebSocket - длительное двустороннее сетевое соединение.'],
    ['группа запусков', 'Группа запусков объединяет записи с одинаковой версией приложения, устройством и другими условиями.'],
    ['надежность', 'Надёжность показывает, насколько вывод подтверждён данными. Это не вероятность ошибки.'],
    ['медиана', 'Медиана - середина отсортированной выборки: половина значений ниже, половина выше.'],
    ['перцентиль', 'Перцентиль показывает границу, ниже которой находится заданная доля измерений.'],
    ['retention', 'Retention означает, что объект остаётся достижимым дольше ожидаемого. Это ещё не доказанная утечка.'],
    ['удержание', 'Удержание означает, что объект остаётся достижимым дольше ожидаемого. Это ещё не доказанная утечка.'],
    ['δ', 'Δ - разница между проверяемым и базовым значением.'],
  ]);

  const normalizeHelpLabel = (value) =>
    (value || '').trim().toLocaleLowerCase('ru').replaceAll('ё', 'е');

  const reportHelpFor = (label) => {
    const normalized = normalizeHelpLabel(label);
    if (!normalized) return '';
    const exact = reportTermHelp.get(normalized);
    if (exact) return exact;
    const words = normalized.split(/[^a-zа-я0-9]+/u);
    for (const word of words) {
      const help = reportTermHelp.get(word);
      if (help) return help;
    }
    return '';
  };

  const enhanceReportHelp = (root = document) => {
    tableScope(root).querySelectorAll('th').forEach((header) => {
      if (header.dataset.tip) return;
      const label = header.textContent.trim().replace(/\s+/g, ' ');
      if (!label) return;
      const explicit = header.querySelector('[data-tip]');
      header.dataset.tip = explicit?.dataset.tip || reportHelpFor(label) ||
        `Столбец «${label}»: наведите на значение ниже, чтобы прочитать его полностью.`;
      if (!header.hasAttribute('tabindex')) header.tabIndex = 0;
    });
    tableScope(root).querySelectorAll(
      'h1, h2, h3, h4, .label, .env-label, .pill, .chip, .influence-toolbar-label'
    ).forEach((label) => {
      if (label.dataset.tip) return;
      const help = reportHelpFor(label.textContent);
      if (!help) return;
      label.dataset.tip = help;
      if (!label.matches('a, button, input, select, textarea, [tabindex]')) label.tabIndex = 0;
    });
  };

  enhanceReportHelp();

  const tooltip = document.createElement('div');
  tooltip.className = 'jh-tooltip';
  tooltip.id = 'jh-report-tooltip';
  tooltip.setAttribute('role', 'tooltip');
  document.body.appendChild(tooltip);
  let activeTarget = null;
  const gap = 10;
  const margin = 12;

  const clamp = (value, min, max) => Math.min(Math.max(value, min), max);

  const viewportBox = () => {
    const viewport = window.visualViewport;
    if (!viewport) {
      return { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
    }
    return {
      left: viewport.offsetLeft,
      top: viewport.offsetTop,
      right: viewport.offsetLeft + viewport.width,
      bottom: viewport.offsetTop + viewport.height,
    };
  };

  const placeTooltip = (target) => {
    const text = normalizeTooltipText(target.dataset.tip || target.getAttribute('aria-label') || target.title || '');
    if (!text) {
      hideTooltip();
      return;
    }
    tooltip.textContent = text;
    tooltip.classList.add('is-visible');
    const rect = target.getBoundingClientRect();
    const tipRect = tooltip.getBoundingClientRect();
    const viewport = viewportBox();
    const centerLeft = rect.left + rect.width / 2 - tipRect.width / 2;
    const middleTop = rect.top + rect.height / 2 - tipRect.height / 2;
    const placements = [
      { name: 'top', left: centerLeft, top: rect.top - tipRect.height - gap },
      { name: 'right', left: rect.right + gap, top: middleTop },
      { name: 'bottom', left: centerLeft, top: rect.bottom + gap },
      { name: 'left', left: rect.left - tipRect.width - gap, top: middleTop },
    ];
    const fits = (placement) =>
      placement.left >= viewport.left + margin &&
      placement.top >= viewport.top + margin &&
      placement.left + tipRect.width <= viewport.right - margin &&
      placement.top + tipRect.height <= viewport.bottom - margin;
    const placement = placements.find(fits) || placements[2];
    const maxLeft = Math.max(viewport.left + margin, viewport.right - tipRect.width - margin);
    const maxTop = Math.max(viewport.top + margin, viewport.bottom - tipRect.height - margin);
    const left = clamp(placement.left, viewport.left + margin, maxLeft);
    const top = clamp(placement.top, viewport.top + margin, maxTop);
    tooltip.dataset.placement = placement.name;
    tooltip.style.left = left + 'px';
    tooltip.style.top = top + 'px';
  };

  const showTooltip = (target) => {
    if (activeTarget && activeTarget !== target) {
      const remaining = (activeTarget.getAttribute('aria-describedby') || '')
        .split(/\s+/).filter((id) => id && id !== tooltip.id);
      if (remaining.length) activeTarget.setAttribute('aria-describedby', remaining.join(' '));
      else activeTarget.removeAttribute('aria-describedby');
    }
    activeTarget = target;
    const describedBy = (target.getAttribute('aria-describedby') || '').split(/\s+/).filter(Boolean);
    if (!describedBy.includes(tooltip.id)) describedBy.push(tooltip.id);
    target.setAttribute('aria-describedby', describedBy.join(' '));
    placeTooltip(target);
  };

  const tooltipTarget = (node) => {
    if (!node?.closest) return null;
    const explicit = node.closest('[data-tip]');
    if (explicit) return explicit;
    const candidate = node.closest(
      'td, th, code, .metric, .section-overview-card, .heuristic-card, .problem-coverage-card'
    );
    if (!candidate || candidate.querySelector('details, table, canvas, svg, input, select, textarea, .cell-toggle')) {
      return null;
    }
    const text = candidate.matches('code') ? candidate.textContent.trim() : readableTooltipText(candidate);
    const descriptiveBlock = candidate.matches(
      '.metric, .section-overview-card, .heuristic-card, .problem-coverage-card'
    );
    const overflows = candidate.scrollWidth > candidate.clientWidth + 3 || candidate.scrollHeight > candidate.clientHeight + 3;
    if (!text || (!candidate.matches('code') && !descriptiveBlock && !overflows && text.length <= 80)) return null;
    candidate.dataset.tip = text;
    if (!candidate.matches('a, button, input, select, textarea, [tabindex]')) {
      candidate.tabIndex = 0;
    }
    return candidate;
  };

  const hideTooltip = () => {
    if (activeTarget) {
      const remaining = (activeTarget.getAttribute('aria-describedby') || '')
        .split(/\s+/).filter((id) => id && id !== tooltip.id);
      if (remaining.length) activeTarget.setAttribute('aria-describedby', remaining.join(' '));
      else activeTarget.removeAttribute('aria-describedby');
    }
    activeTarget = null;
    tooltip.classList.remove('is-visible');
  };

  document.addEventListener('pointerover', (event) => {
    const target = tooltipTarget(event.target);
    if (target) showTooltip(target);
  });
  document.addEventListener('pointerout', (event) => {
    const fromTarget = event.target.closest('[data-tip]');
    const toTarget = event.relatedTarget && event.relatedTarget.closest
      ? event.relatedTarget.closest('[data-tip]')
      : null;
    if (fromTarget && fromTarget === activeTarget && !toTarget) {
      hideTooltip();
    }
  });
  document.addEventListener('focusin', (event) => {
    const target = tooltipTarget(event.target);
    if (target) showTooltip(target);
  });
  document.addEventListener('focusout', hideTooltip);
  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && activeTarget) hideTooltip();
  });
  window.addEventListener('scroll', () => {
    if (activeTarget) placeTooltip(activeTarget);
  }, { passive: true });
  window.addEventListener('resize', () => {
    if (activeTarget) placeTooltip(activeTarget);
    scheduleTableMeasure();
  }, { passive: true });
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', () => {
      if (activeTarget) placeTooltip(activeTarget);
    }, { passive: true });
  }

  const revealHashTarget = (hash = window.location.hash) => {
    if (!hash || hash === '#') return;
    let id = hash.slice(1);
    try {
      id = decodeURIComponent(id);
    } catch (_) {
      return;
    }
    const target = document.getElementById(id);
    if (!target) return;
    let details = target.closest('details');
    while (details) {
      details.open = true;
      details = details.parentElement?.closest('details');
    }
    document.querySelectorAll('.nav a[href^="#"]').forEach((link) => {
      const active = link.hash === hash;
      link.classList.toggle('active', active);
      if (active) link.setAttribute('aria-current', 'location');
      else link.removeAttribute('aria-current');
    });
    target.scrollIntoView({ behavior: 'smooth', block: 'start' });
  };

  document.addEventListener('click', (event) => {
    const link = event.target.closest?.('a[href^="#"]');
    if (!link) return;
    requestAnimationFrame(() => revealHashTarget(link.hash));
  });
  window.addEventListener('hashchange', () => revealHashTarget());
  if (window.location.hash) requestAnimationFrame(() => revealHashTarget());

  document.querySelectorAll('[data-zero-toggle]').forEach((toggle) => {
    const scope = toggle.closest('[data-zero-scope]') || document.body;
    const apply = () => scope.classList.toggle('show-zero-buckets', toggle.checked);
    toggle.addEventListener('change', apply);
    apply();
  });

  const appendCodeEvidenceBlock = (body, title, className = '') => {
    const block = document.createElement('div');
    block.className = 'code-problem-detail-block' + (className ? ' ' + className : '');
    const heading = document.createElement('strong');
    heading.textContent = title;
    block.appendChild(heading);
    body.appendChild(block);
    return block;
  };
  const appendTextParagraph = (parent, value) => {
    const paragraph = document.createElement('p');
    paragraph.textContent = value || 'нет данных';
    parent.appendChild(paragraph);
  };
  const codeEvidenceMetric = (signal) => {
    const parts = [];
    if (signal.count) parts.push('кол-во ' + signal.count);
    if (signal.total_ms) parts.push('итого ' + signal.total_ms + ' мс');
    if (signal.max_ms) parts.push('макс. ' + signal.max_ms + ' мс');
    if (signal.value) parts.push(signal.value + ' ' + (signal.unit || 'значение'));
    return parts.length ? parts.join(' · ') : 'сигнал';
  };
  const codeEvidenceDrillPath = (drill) => {
    let location = drill.class_name || 'класс не определен';
    if (drill.method) location += '.' + drill.method;
    const context = [];
    if (drill.screen) context.push('экран ' + drill.screen);
    if (drill.operation) context.push('операция ' + drill.operation);
    if (drill.route) context.push('маршрут ' + drill.route);
    return context.length ? location + ' -> ' + context.join(' -> ') : location;
  };
  const codeEvidenceArchives = new WeakMap();
  const codeEvidenceSearchTexts = new WeakMap();
  const reportCodeArchiveFailure = (error) => {
    console.error('Не удалось прочитать архив с доказательствами по коду', error);
  };
  const normalizeCodeEvidenceSearch = (value) => (value || '')
    .normalize('NFKC')
    .trim()
    .toLocaleLowerCase('ru')
    .replaceAll('ё', 'е');
  const codeEvidenceLocation = (problem) => {
    const className = problem?.class_name || 'класс не определён';
    return className + (problem?.method ? '.' + problem.method : '');
  };
  const codeEvidenceRecordSearch = (record) => {
    const cached = codeEvidenceSearchTexts.get(record);
    if (cached) return cached;
    const text = normalizeCodeEvidenceSearch(JSON.stringify(record));
    codeEvidenceSearchTexts.set(record, text);
    return text;
  };
  const codeEvidenceKey = (problem) => {
    const className = problem?.class_name || '';
    const byteLength = new TextEncoder().encode(className).length;
    return byteLength + ':' + className + (problem?.method || '');
  };
  const loadCodeEvidenceArchive = (registry) => {
    if (!registry) return Promise.resolve(null);
    const active = codeEvidenceArchives.get(registry);
    if (active) return active;
    const payload = registry.querySelector('script[data-code-problem-evidence-archive]');
    if (!payload) return Promise.resolve(null);
    const promise = (async () => {
      if (payload.dataset.encoding !== 'gzip-base64url' || typeof DecompressionStream !== 'function') {
        throw new Error('gzip evidence decompression is unavailable');
      }
      const rawPayload = (payload.textContent || '').trim();
      const encoded = (rawPayload.startsWith('"') ? JSON.parse(rawPayload) : rawPayload).replace(/\s+/g, '');
      const normalized = encoded.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(encoded.length / 4) * 4, '=');
      const binary = atob(normalized);
      const compressed = new Uint8Array(binary.length);
      for (let index = 0; index < binary.length; index += 1) compressed[index] = binary.charCodeAt(index);
      const stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream('gzip'));
      const archive = await new Response(stream).json();
      const records = archive.mode === 'compare'
        ? (archive.rows || []).map((compare) => ({ problem: compare.candidate, compare }))
        : (archive.problems || []).map((problem) => ({ problem, compare: null }));
      const byKey = new Map();
      records.forEach((record) => byKey.set(codeEvidenceKey(record.problem), record));
      payload.remove();
      return { mode: archive.mode || 'inspect', records, byKey };
    })();
    codeEvidenceArchives.set(registry, promise);
    void promise.catch(() => {
      if (codeEvidenceArchives.get(registry) === promise) codeEvidenceArchives.delete(registry);
    });
    return promise;
  };
  const materializeCodeProblemEvidence = async (details) => {
    if (!details || details.dataset.evidenceLoaded === 'true') return;
    const body = details.querySelector('[data-code-problem-evidence-body]');
    if (!body) return;
    try {
      const archive = await loadCodeEvidenceArchive(details.closest('[data-code-registry]'));
      const problem = archive?.byKey.get(details.dataset.codeProblemEvidenceKey || '')?.problem;
      if (!problem) throw new Error('code problem evidence is missing');
      renderCodeProblemEvidence(body, problem);
      details.dataset.evidenceLoaded = 'true';
      scheduleTableMeasure();
    } catch (error) {
      body.textContent = 'Не удалось прочитать полные доказательства.';
      console.error(error);
    }
  };
  const renderCodeProblemEvidence = (body, problem) => {
    body.replaceChildren();

    appendTextParagraph(appendCodeEvidenceBlock(body, 'Доказательство', 'span-all'), problem.evidence);
    const contextBlock = appendCodeEvidenceBlock(body, 'Контекст');
    const context = document.createElement('div');
    context.className = 'problem-context';
    const appendContext = (label, values) => (values || []).forEach((value) => {
      const row = document.createElement('div');
      row.append(document.createTextNode(label + ' '));
      const code = document.createElement('code');
      code.textContent = value;
      row.appendChild(code);
      context.appendChild(row);
    });
    appendContext('экран', problem.screens);
    appendContext('операция', problem.operations);
    appendContext('маршрут', problem.routes);
    if (!context.childNodes.length) context.textContent = 'контекст не записан';
    contextBlock.appendChild(context);

    appendTextParagraph(
      appendCodeEvidenceBlock(body, body.dataset.impactLabel || 'Влияние'),
      problem.impact,
    );
    appendTextParagraph(appendCodeEvidenceBlock(body, 'Что проверить', 'span-all'), problem.recommendation);

    if ((problem.drill_down || []).length) {
      const drillBlock = appendCodeEvidenceBlock(body, 'Детализация', 'span-all');
      const drilldown = document.createElement('div');
      drilldown.className = 'problem-drilldown';
      problem.drill_down.forEach((drill) => {
        const item = document.createElement('div');
        item.className = 'problem-drill';
        const heading = document.createElement('strong');
        heading.textContent = codeEvidenceDrillPath(drill);
        const evidence = document.createElement('span');
        evidence.textContent = 'Доказательство: ' + (drill.evidence || 'нет данных');
        const recommendation = document.createElement('span');
        recommendation.textContent = 'Рекомендация: ' + (drill.recommendation || 'нет данных');
        item.append(heading, evidence, recommendation);
        drilldown.appendChild(item);
      });
      drillBlock.appendChild(drilldown);
    }

    const signalBlock = appendCodeEvidenceBlock(body, body.dataset.signalsLabel || 'Сигналы', 'span-all');
    const signals = document.createElement('div');
    signals.className = 'problem-signals';
    (problem.signals || []).forEach((signal) => {
      const item = document.createElement('div');
      const severity = signal.severity === 'high' || signal.severity === 'medium' ? signal.severity : 'ok';
      item.className = 'problem-signal sev-' + severity;
      const heading = document.createElement('strong');
      heading.textContent = signal.name || 'Сигнал';
      const detail = document.createElement('small');
      detail.append(document.createTextNode((signal.category || 'сигнал') + ' · ' + codeEvidenceMetric(signal)));
      detail.appendChild(document.createElement('br'));
      detail.append(document.createTextNode(signal.detail || ''));
      item.append(heading, detail);
      signals.appendChild(item);
    });
    if (!signals.childNodes.length) signals.textContent = 'Нет дополнительных сигналов.';
    signalBlock.appendChild(signals);
  };
  document.addEventListener('toggle', (event) => {
    const details = event.target.closest?.('.code-problem-details[data-evidence-loaded], .code-problem-details');
    if (details?.open) void materializeCodeProblemEvidence(details);
  }, true);

  const createCodeEvidenceElement = (tag, className, text) => {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = text;
    return element;
  };
  const codeEvidenceSeverityLabel = (severity, lowRisk = false) => {
    if (severity === 'high') return 'критично';
    if (severity === 'medium') return 'предупреждение';
    return lowRisk ? 'низкий риск' : 'норма';
  };
  const buildCodeEvidenceDetails = (problem, compareMode) => {
    const details = createCodeEvidenceElement('details', 'code-problem-details');
    details.dataset.codeProblemEvidenceKey = codeEvidenceKey(problem);
    const summary = document.createElement('summary');
    const main = createCodeEvidenceElement('span', 'code-problem-summary-main');
    main.append(
      createCodeEvidenceElement('strong', '', 'Доказательства и рекомендация'),
      createCodeEvidenceElement('em', '', problem.evidence || ''),
    );
    const signalCount = (problem.signals || []).length;
    const drillCount = (problem.drill_down || []).length;
    const count = createCodeEvidenceElement('small', '', signalCount + ' сигналов' + (drillCount ? ' · ' + drillCount + ' сценариев' : ''));
    summary.append(main, count);
    const body = createCodeEvidenceElement('div', 'code-problem-detail-body');
    body.dataset.codeProblemEvidenceBody = '';
    body.dataset.impactLabel = compareMode ? 'Влияние в проверяемом прогоне' : 'Влияние';
    body.dataset.signalsLabel = compareMode ? 'Сигналы проверяемого прогона' : 'Сигналы';
    const placeholder = appendCodeEvidenceBlock(body, 'Полные доказательства', 'span-all');
    appendTextParagraph(placeholder, 'Откройте строку, чтобы развернуть все сигналы и сценарии без усечения.');
    details.append(summary, body);
    return details;
  };
  const buildCodeProblemRow = (record, mode) => {
    const problem = record.problem;
    const compare = record.compare;
    const compareMode = mode === 'compare';
    const severity = compareMode ? (compare.severity || problem.severity) : problem.severity;
    const score = compareMode ? Number(compare.delta_score || 0) : Number(problem.score || 0);
    const location = codeEvidenceLocation(problem);
    const row = document.createElement('tr');
    row.dataset.codeProblemRow = '';
    row.dataset.score = score.toFixed(1);
    row.dataset.severity = severity || 'ok';
    row.dataset.class = location;
    row.dataset.categories = (problem.categories || []).join('|');

    if (compareMode) {
      const status = document.createElement('td');
      status.append(
        createCodeEvidenceElement('span', 'problem-score sev-' + (severity || 'ok'), codeEvidenceSeverityLabel(severity, true)),
        createCodeEvidenceElement('div', 'muted', compare.status || ''),
      );
      row.appendChild(status);
    } else {
      const scoreCell = document.createElement('td');
      scoreCell.append(
        createCodeEvidenceElement('span', 'problem-score sev-' + (severity || 'ok'), Number(problem.score || 0).toFixed(1)),
        createCodeEvidenceElement('div', 'muted', codeEvidenceSeverityLabel(severity, true)),
        createCodeEvidenceElement('div', 'muted', problem.runtime_evidence ? 'есть данные выполнения' : 'только статическая связь'),
      );
      row.appendChild(scoreCell);
    }

    const locationCell = document.createElement('td');
    const locationBlock = createCodeEvidenceElement('div', 'problem-location');
    locationBlock.appendChild(createCodeEvidenceElement('code', '', problem.class_name || ''));
    if (problem.method) {
      const method = createCodeEvidenceElement('div', 'method');
      method.appendChild(createCodeEvidenceElement('code', '', problem.method));
      locationBlock.appendChild(method);
    }
    locationCell.appendChild(locationBlock);
    row.appendChild(locationCell);

    if (compareMode) {
      const scoreCell = document.createElement('td');
      scoreCell.appendChild(createCodeEvidenceElement('div', '', 'база ' + (compare.has_baseline ? Number(compare.baseline_score || 0).toFixed(1) : 'нет сопоставимой точки')));
      scoreCell.appendChild(createCodeEvidenceElement('div', '', 'проверяемый ' + Number(problem.score || 0).toFixed(1)));
      scoreCell.appendChild(createCodeEvidenceElement('div', 'muted', compare.comparable ? 'дельта ' + (score >= 0 ? '+' : '') + score.toFixed(1) : 'дельта не вычисляется'));
      row.appendChild(scoreCell);
    }

    const categoriesCell = document.createElement('td');
    const tags = createCodeEvidenceElement('div', 'problem-tags');
    (problem.categories || []).forEach((category) => tags.appendChild(createCodeEvidenceElement('span', 'problem-chip', category)));
    categoriesCell.append(tags, createCodeEvidenceElement('div', 'muted', (problem.problems || []).join(', ')));
    row.appendChild(categoriesCell);
    const detailsCell = document.createElement('td');
    detailsCell.appendChild(buildCodeEvidenceDetails(problem, compareMode));
    row.appendChild(detailsCell);
    return row;
  };
  const setupArchivedCodeRegistry = (registry) => {
    const tbody = registry.querySelector('tbody');
    const search = registry.querySelector('[data-code-registry-search]');
    const severity = registry.querySelector('[data-code-registry-severity]');
    const category = registry.querySelector('[data-code-registry-category]');
    const counter = registry.querySelector('[data-code-registry-count]');
    const sortButtons = Array.from(registry.querySelectorAll('[data-code-sort]'));
    const registryScope = registry.closest('.fold-body, .panel, .details-body, .report-section') || registry.parentElement || registry;
    const categoryButtons = Array.from(registryScope.querySelectorAll('[data-registry-category]'));
    const severityButtons = Array.from(registryScope.querySelectorAll('[data-registry-severity]'));
    const mode = registry.dataset.codeProblemMode || 'inspect';
    const severityRank = { high: 3, medium: 2, ok: 1 };
    let sortKey = mode === 'compare' ? 'severity' : 'score';
    let sortDir = 'desc';
    let activeRecords = null;
    let filterFrame = 0;
    const initialLoader = tbody.querySelector('[data-code-problem-loader]');
    const total = Number(initialLoader?.dataset.deferredTotal || tbody.querySelectorAll('[data-code-problem-row]').length);
    if (counter) counter.textContent = Math.min(50, total) + ' из ' + total;

    const recordSeverity = (record) => mode === 'compare'
      ? (record.compare?.severity || record.problem.severity || 'ok')
      : (record.problem.severity || 'ok');
    const recordScore = (record) => mode === 'compare'
      ? Number(record.compare?.delta_score || 0)
      : Number(record.problem.score || 0);
    const recordClass = (record) => codeEvidenceLocation(record.problem);
    const recordCategories = (record) => (record.problem.categories || []).join('|');
    const compareRecords = (left, right) => {
      let a;
      let b;
      if (sortKey === 'score') {
        a = recordScore(left);
        b = recordScore(right);
      } else if (sortKey === 'severity') {
        a = severityRank[recordSeverity(left)] || 0;
        b = severityRank[recordSeverity(right)] || 0;
      } else if (sortKey === 'class') {
        a = recordClass(left);
        b = recordClass(right);
      } else {
        a = recordCategories(left);
        b = recordCategories(right);
      }
      const result = typeof a === 'number' ? a - b : String(a).localeCompare(String(b), 'ru');
      return sortDir === 'asc' ? result : -result;
    };
    const updateControls = (matchCount, loadedCount) => {
      if (counter) counter.textContent = matchCount + ' из ' + total;
      const categoryValue = category?.value || '';
      const severityValue = severity?.value || '';
      categoryButtons.forEach((button) => button.classList.toggle('is-active', Boolean(categoryValue) && button.dataset.registryCategory === categoryValue));
      severityButtons.forEach((button) => button.classList.toggle('is-active', Boolean(severityValue) && button.dataset.registrySeverity === severityValue));
      sortButtons.forEach((button) => {
        const active = button.dataset.codeSort === sortKey;
        button.classList.toggle('active', active);
        button.classList.toggle('asc', active && sortDir === 'asc');
        button.classList.toggle('desc', active && sortDir === 'desc');
      });
      registry.classList.toggle('no-results', matchCount === 0);
      const remaining = Math.max(0, matchCount - loadedCount);
      if (remaining > 0) {
        const columns = mode === 'compare' ? 5 : 4;
        const loader = createCodeEvidenceElement('tr', 'deferred-table-loader');
        loader.dataset.codeProblemLoader = '';
        loader.dataset.deferredTotal = String(matchCount);
        loader.dataset.deferredLoaded = String(loadedCount);
        const cell = document.createElement('td');
        cell.colSpan = columns;
        const next = Math.min(50, remaining);
        const button = createCodeEvidenceElement('button', 'deferred-table-button', 'Показать ещё ' + next);
        button.type = 'button';
        button.dataset.loadMoreCodeProblems = '';
        cell.append(button, createCodeEvidenceElement('span', '', 'Осталось строк: ' + remaining));
        cell.lastChild.dataset.deferredRemaining = '';
        loader.appendChild(cell);
        tbody.appendChild(loader);
      }
      scheduleTableMeasure();
    };
    const renderRecords = (records, loadedCount) => {
      tbody.replaceChildren();
      records.slice(0, loadedCount).forEach((record) => tbody.appendChild(buildCodeProblemRow(record, mode)));
      updateControls(records.length, Math.min(loadedCount, records.length));
      enhanceLongCells(tbody);
    };
    const applyArchive = async () => {
      const archive = await loadCodeEvidenceArchive(registry);
      if (!archive) return;
      const query = (search?.value || '').trim().toLowerCase();
      const severityValue = severity?.value || '';
      const categoryValue = category?.value || '';
      activeRecords = archive.records.filter((record) =>
        (!query || codeEvidenceRecordSearch(record).includes(query)) &&
        (!severityValue || recordSeverity(record) === severityValue) &&
        (!categoryValue || (record.problem.categories || []).includes(categoryValue))
      );
      activeRecords.sort(compareRecords);
      renderRecords(activeRecords, Math.min(50, activeRecords.length));
    };
    const scheduleApply = () => {
      if (filterFrame) cancelAnimationFrame(filterFrame);
      filterFrame = requestAnimationFrame(() => {
        filterFrame = 0;
        void applyArchive().catch(reportCodeArchiveFailure);
      });
    };
    search?.addEventListener('input', scheduleApply);
    severity?.addEventListener('change', scheduleApply);
    category?.addEventListener('change', scheduleApply);
    categoryButtons.forEach((button) => button.addEventListener('click', () => {
      if (!category) return;
      category.value = category.value === (button.dataset.registryCategory || '') ? '' : (button.dataset.registryCategory || '');
      scheduleApply();
    }));
    severityButtons.forEach((button) => button.addEventListener('click', () => {
      if (!severity) return;
      severity.value = severity.value === (button.dataset.registrySeverity || '') ? '' : (button.dataset.registrySeverity || '');
      scheduleApply();
    }));
    sortButtons.forEach((button) => button.addEventListener('click', () => {
      const nextKey = button.dataset.codeSort;
      if (sortKey === nextKey) sortDir = sortDir === 'asc' ? 'desc' : 'asc';
      else {
        sortKey = nextKey;
        sortDir = nextKey === 'class' || nextKey === 'category' ? 'asc' : 'desc';
      }
      scheduleApply();
    }));
    registry.addEventListener('click', async (event) => {
      const button = event.target.closest('[data-load-more-code-problems]');
      if (!button) return;
      button.disabled = true;
      try {
        if (!activeRecords) {
          const archive = await loadCodeEvidenceArchive(registry);
          activeRecords = archive?.records || [];
        }
        const loaded = tbody.querySelectorAll('[data-code-problem-row]').length;
        renderRecords(activeRecords, Math.min(loaded + 50, activeRecords.length));
      } catch (error) {
        button.disabled = false;
        reportCodeArchiveFailure(error);
      }
    });
  };

  document.querySelectorAll('[data-code-registry]').forEach((registry) => {
    if (registry.dataset.codeProblemMode) {
      setupArchivedCodeRegistry(registry);
      return;
    }
    const tbody = registry.querySelector('tbody');
    let sortedRows = Array.from(registry.querySelectorAll('[data-code-problem-row]'));
    const search = registry.querySelector('[data-code-registry-search]');
    const severity = registry.querySelector('[data-code-registry-severity]');
    const category = registry.querySelector('[data-code-registry-category]');
    const counter = registry.querySelector('[data-code-registry-count]');
    const sortButtons = Array.from(registry.querySelectorAll('[data-code-sort]'));
    const registryScope = registry.closest('.fold-body, .panel, .details-body, .report-section') || registry.parentElement || registry;
    const categoryButtons = Array.from(registryScope.querySelectorAll('[data-registry-category]'));
    const severityButtons = Array.from(registryScope.querySelectorAll('[data-registry-severity]'));
    const severityRank = { high: 3, medium: 2, ok: 1 };
    let sortKey = 'score';
    let sortDir = 'desc';
    let filterFrame = 0;
    const deferredTotal = Number(tbody.querySelector('[data-deferred-loader]')?.dataset.deferredTotal || sortedRows.length);
    const ensureSelectOption = (select, value, label) => {
      if (!select || !value || Array.from(select.options).some((option) => option.value === value)) return;
      select.appendChild(new Option(label || value, value));
    };
    const chipLabel = (button) => button.textContent.trim().replace(/\s+\d+$/, '').trim();
    const setSelectFromChip = (select, value, label) => {
      if (!select) return;
      ensureSelectOption(select, value, label);
      select.value = select.value === value ? '' : value;
      select.dispatchEvent(new Event('change', { bubbles: true }));
    };
    const valueFor = (row, key) => {
      if (key === 'score') return Number(row.dataset.score || 0);
      if (key === 'severity') return severityRank[row.dataset.severity] || 0;
      if (key === 'class') return row.dataset.class || '';
      if (key === 'category') return row.dataset.categories || '';
      return row.dataset.search || row.textContent || '';
    };
    const compareValues = (a, b) => {
      const av = valueFor(a, sortKey);
      const bv = valueFor(b, sortKey);
      if (typeof av === 'number' && typeof bv === 'number') return av - bv;
      return String(av).localeCompare(String(bv), 'ru');
    };
    const sortRows = () => {
      sortedRows.sort((a, b) => {
        const result = compareValues(a, b);
        return sortDir === 'asc' ? result : -result;
      });
    };
    const apply = (reorder = false) => {
      const query = (search?.value || '').trim().toLowerCase();
      const severityValue = severity?.value || '';
      const categoryValue = category?.value || '';
      let visible = 0;
      sortedRows.forEach((row) => {
        if (!row.jhSearchText) row.jhSearchText = (row.dataset.search || row.textContent || '').toLowerCase();
        const searchableText = row.jhSearchText;
        const matchesQuery = !query || searchableText.includes(query);
        const matchesSeverity = !severityValue || row.dataset.severity === severityValue;
        const matchesCategory = !categoryValue || (row.dataset.categories || '').split('|').includes(categoryValue);
        const hidden = !(matchesQuery && matchesSeverity && matchesCategory);
        row.hidden = hidden;
        if (!hidden) visible += 1;
        if (reorder) {
          const anchor = tbody.querySelector('script[data-table-chunk], [data-deferred-loader]');
          tbody.insertBefore(row, anchor);
        }
      });
      registry.classList.toggle('no-results', visible === 0);
      if (counter) counter.textContent = visible + ' из ' + deferredTotal;
      categoryButtons.forEach((button) => {
        button.classList.toggle('is-active', Boolean(categoryValue) && button.dataset.registryCategory === categoryValue);
      });
      severityButtons.forEach((button) => {
        button.classList.toggle('is-active', Boolean(severityValue) && button.dataset.registrySeverity === severityValue);
      });
      sortButtons.forEach((button) => {
        const active = button.dataset.codeSort === sortKey;
        button.classList.toggle('active', active);
        button.classList.toggle('asc', active && sortDir === 'asc');
        button.classList.toggle('desc', active && sortDir === 'desc');
      });
      scheduleTableMeasure();
    };
    const applyWithCompleteData = async (reorder = false) => {
      try {
        if (nextDeferredChunk(tbody)) {
          await materializeAllDeferredRows(tbody);
        }
        if ((search?.value || '').trim()) {
          const archive = await loadCodeEvidenceArchive(registry);
          if (archive) {
            sortedRows.forEach((row) => {
              if (row.jhSearchText) return;
              const details = row.querySelector('[data-code-problem-evidence-key]');
              const problem = archive.byKey.get(details?.dataset.codeProblemEvidenceKey || '')?.problem;
              row.jhSearchText = ((row.dataset.search || row.textContent || '') + ' ' + JSON.stringify(problem || '')).toLowerCase();
            });
          }
        }
      } catch (error) {
        reportCodeArchiveFailure(error);
        return;
      }
      if (reorder) sortRows();
      apply(reorder);
    };
    const scheduleFilter = () => {
      if (filterFrame) cancelAnimationFrame(filterFrame);
      filterFrame = requestAnimationFrame(async () => {
        filterFrame = 0;
        await applyWithCompleteData();
      });
    };
    search?.addEventListener('input', scheduleFilter);
    severity?.addEventListener('change', () => applyWithCompleteData());
    category?.addEventListener('change', () => applyWithCompleteData());
    categoryButtons.forEach((button) => {
      button.addEventListener('click', () => {
        const value = button.dataset.registryCategory || '';
        setSelectFromChip(category, value, chipLabel(button));
      });
    });
    severityButtons.forEach((button) => {
      button.addEventListener('click', () => {
        const value = button.dataset.registrySeverity || '';
        setSelectFromChip(severity, value, chipLabel(button));
      });
    });
    sortButtons.forEach((button) => {
      button.addEventListener('click', async () => {
        const nextKey = button.dataset.codeSort;
        if (sortKey === nextKey) {
          sortDir = sortDir === 'asc' ? 'desc' : 'asc';
        } else {
          sortKey = nextKey;
          sortDir = nextKey === 'class' || nextKey === 'category' ? 'asc' : 'desc';
        }
        await applyWithCompleteData(true);
      });
    });
    registry.addEventListener('report:rows-added', (event) => {
      const additions = (event.detail?.rows || []).filter((row) => row.matches('[data-code-problem-row]'));
      if (!additions.length) return;
      sortedRows = sortedRows.concat(additions);
      sortRows();
      apply(true);
    });
    sortRows();
    apply(true);
  });

  const archivedCodeSearchEntries = async () => {
    const registries = Array.from(document.querySelectorAll('[data-code-registry][data-code-problem-mode]'));
    const entries = [];
    for (const registry of registries) {
      const archive = await loadCodeEvidenceArchive(registry);
      if (!archive) continue;
      const liveKeys = new Set(
        Array.from(registry.querySelectorAll('[data-code-problem-evidence-key]'))
          .map((details) => details.dataset.codeProblemEvidenceKey || ''),
      );
      const section = registry.closest('section[id]');
      const sectionTitle = section?.querySelector('h2')?.textContent?.trim() || 'Реестр кода';
      archive.records.forEach((record) => {
        const key = codeEvidenceKey(record.problem);
        if (liveKeys.has(key)) return;
        const location = codeEvidenceLocation(record.problem);
        entries.push({
          sectionTitle,
          label: location,
          text: codeEvidenceRecordSearch(record),
          normalized: true,
          reveal: async () => {
            const findRow = () => Array.from(registry.querySelectorAll('[data-code-problem-row]'))
              .find((row) => row.querySelector('[data-code-problem-evidence-key]')?.dataset.codeProblemEvidenceKey === key);
            let row = findRow();
            if (row) return row;
            const search = registry.querySelector('[data-code-registry-search]');
            if (!search) return null;
            search.value = location;
            search.dispatchEvent(new Event('input', { bubbles: true }));
            for (let attempt = 0; attempt < 180; attempt += 1) {
              await new Promise((resolveTick) => requestAnimationFrame(resolveTick));
              row = findRow();
              if (row) return row;
            }
            return null;
          },
        });
      });
    }
    return entries;
  };

  if (typeof initializeProblemSearch === 'function') {
    initializeProblemSearch(revealDeferredSearchEntry, archivedCodeSearchEntries);
  }

  document.querySelectorAll('[data-leak-explorer]').forEach((explorer) => {
    const buttons = Array.from(explorer.querySelectorAll('[data-leak-select]'));
    const panels = Array.from(explorer.querySelectorAll('[data-leak-panel]'));
    const panelIDs = new Set(panels.map((panel) => panel.id));
    const linkedRows = Array.from(document.querySelectorAll('[data-leak-row]'))
      .filter((row) => panelIDs.has(row.dataset.leakTarget || ''));
    const tablist = explorer.querySelector('.leak-selector');
    if (tablist) tablist.setAttribute('role', 'tablist');
    const activate = (targetID) => {
      let matched = false;
      panels.forEach((panel) => {
        const active = panel.id === targetID;
        panel.hidden = !active;
        panel.classList.toggle('is-active', active);
        panel.setAttribute('aria-hidden', String(!active));
        panel.querySelectorAll('[data-leak-node]').forEach((node) => {
          if (active) {
            if (node.dataset.tipCache && !node.dataset.tip) {
              node.dataset.tip = node.dataset.tipCache;
            }
            node.tabIndex = 0;
          } else {
            if (node.dataset.tip) {
              node.dataset.tipCache = node.dataset.tip;
              delete node.dataset.tip;
            }
            node.tabIndex = -1;
          }
        });
        if (active) matched = true;
      });
      buttons.forEach((button) => {
        const active = button.dataset.leakTarget === targetID;
        button.classList.toggle('is-active', active);
        button.setAttribute('aria-selected', String(active));
        button.tabIndex = active ? 0 : -1;
      });
      linkedRows.forEach((row) => {
        row.classList.toggle('is-active', row.dataset.leakTarget === targetID);
      });
      if (!matched && panels[0]) {
        activate(panels[0].id);
      }
      scheduleTableMeasure();
    };
    buttons.forEach((button) => {
      button.addEventListener('click', () => activate(button.dataset.leakTarget));
    });
    linkedRows.forEach((row) => {
      if (row.dataset.leakRowEnhanced === 'true') return;
      row.dataset.leakRowEnhanced = 'true';
      row.tabIndex = 0;
      row.addEventListener('click', () => {
        const fold = explorer.closest('details');
        if (fold && !fold.open) fold.open = true;
        activate(row.dataset.leakTarget);
        explorer.scrollIntoView({ block: 'start', behavior: 'smooth' });
      });
      row.addEventListener('keydown', (event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        row.click();
      });
    });
    if (buttons[0]) {
      activate(buttons[0].dataset.leakTarget);
    }
    explorer.querySelectorAll('[data-leak-node]').forEach((node) => {
      const selectNode = () => {
        const svg = node.closest('svg');
        if (!svg) return;
        svg.querySelectorAll('[data-leak-node]').forEach((other) => {
          other.classList.toggle('is-selected', other === node);
        });
      };
      node.addEventListener('click', selectNode);
      node.addEventListener('keydown', (event) => {
        if (event.key !== 'Enter' && event.key !== ' ') return;
        event.preventDefault();
        selectNode();
      });
    });
  });
})();
