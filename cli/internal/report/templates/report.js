
(() => {
  const modernReport = document.body.dataset.reportStyle !== 'legacy';
  if (modernReport) {
    const explorer = document.getElementById('explorer');
    const registry = document.getElementById('registry');
    if (explorer && registry && explorer.parentNode === registry.parentNode) {
      explorer.parentNode.insertBefore(registry, explorer);
    }
  }

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

  wrapTables();

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
    window.setTimeout(() => callback({ timeRemaining: () => 8 }), 0);
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
    toggle.textContent = modernReport ? 'развернуть' : 'показать полностью';
    toggle.setAttribute('aria-expanded', 'false');
    toggle.addEventListener('click', () => {
      const expanded = !clip.classList.contains('is-expanded');
      clip.classList.toggle('is-expanded', expanded);
      toggle.textContent = expanded ? 'свернуть' : (modernReport ? 'развернуть' : 'показать полностью');
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

  const materializeDeferredChunk = (tbody, announce = true) => {
    const script = nextDeferredChunk(tbody);
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
      const added = [];
      const step = () => {
        const started = performance.now();
        try {
          while (nextDeferredChunk(tbody) && performance.now() - started < 8) {
            added.push(...materializeDeferredChunk(tbody, false));
          }
        } catch (error) {
          reject(error);
          return;
        }
        if (nextDeferredChunk(tbody)) {
          requestAnimationFrame(step);
          return;
        }
        if (added.length) {
          tbody.dispatchEvent(new CustomEvent('report:rows-added', { bubbles: true, detail: { rows: added } }));
        }
        resolve(added);
      };
      requestAnimationFrame(step);
    });
    deferredLoads.set(tbody, promise);
    return promise;
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

  const tooltip = document.createElement('div');
  tooltip.className = 'jh-tooltip';
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
    activeTarget = target;
    placeTooltip(target);
  };

  const tooltipTarget = (node) => {
    const explicit = node.closest('[data-tip]');
    if (explicit) return explicit;
    const candidate = node.closest('td, th, code');
    if (!candidate || candidate.querySelector('details, table, canvas, svg, input, select, textarea, .cell-toggle')) {
      return null;
    }
    const text = candidate.matches('code') ? candidate.textContent.trim() : readableTooltipText(candidate);
    if (!text || (!candidate.matches('code') && text.length <= 80 && !candidate.closest('.metric'))) return null;
    candidate.dataset.tip = text;
    return candidate;
  };

  const hideTooltip = () => {
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

  document.querySelectorAll('[data-zero-toggle]').forEach((toggle) => {
    const scope = toggle.closest('[data-zero-scope]') || document.body;
    const apply = () => scope.classList.toggle('show-zero-buckets', toggle.checked);
    toggle.addEventListener('change', apply);
    apply();
  });

  document.querySelectorAll('[data-code-registry]').forEach((registry) => {
    const tbody = registry.querySelector('tbody');
    let rows = Array.from(registry.querySelectorAll('[data-code-problem-row]'));
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
    let sortedRows = rows.slice();
    let filterFrame = 0;
    const deferredTotal = Number(tbody.querySelector('[data-deferred-loader]')?.dataset.deferredTotal || rows.length);
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
      if (nextDeferredChunk(tbody)) {
        await materializeAllDeferredRows(tbody);
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
      rows = rows.concat(additions);
      sortedRows = sortedRows.concat(additions);
      sortRows();
      apply(true);
    });
    sortRows();
    apply(true);
  });

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
