(() => {
  const formatGrowthBytes = (value) => {
    const bytes = Math.max(0, Number(value) || 0);
    const units = ['Б', 'КиБ', 'МиБ', 'ГиБ'];
    let scaled = bytes;
    let unit = 0;
    while (scaled >= 1024 && unit < units.length - 1) {
      scaled /= 1024;
      unit += 1;
    }
    const digits = unit === 0 || scaled >= 100 ? 0 : 1;
    return `${scaled.toLocaleString('ru-RU', { maximumFractionDigits: digits })} ${units[unit]}`;
  };

  const formatGrowthDuration = (value) => {
    const milliseconds = Math.max(0, Number(value) || 0);
    if (milliseconds < 1000) return `${Math.round(milliseconds)} мс`;
    const seconds = milliseconds / 1000;
    if (seconds < 60) return `${seconds.toLocaleString('ru-RU', { maximumFractionDigits: 1 })} с`;
    const minutes = seconds / 60;
    if (minutes < 60) return `${minutes.toLocaleString('ru-RU', { maximumFractionDigits: 1 })} мин`;
    return `${(minutes / 60).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} ч`;
  };

  const growthDayISO = (dayKey) => {
    const raw = String(dayKey || '').padStart(8, '0');
    if (!/^\d{8}$/.test(raw)) return '';
    return `${raw.slice(0, 4)}-${raw.slice(4, 6)}-${raw.slice(6, 8)}`;
  };

  const growthDayDisplay = (iso) => {
    const parts = String(iso || '').split('-');
    if (parts.length !== 3 || parts.some((part) => !/^\d+$/.test(part))) return '';
    return `${parts[2].padStart(2, '0')}.${parts[1].padStart(2, '0')}.${parts[0].padStart(4, '0')}`;
  };

  const growthDisplayDayISO = (display) => {
    const match = /^(\d{2})\.(\d{2})\.(\d{4})$/.exec(String(display || '').trim());
    if (!match) return '';
    const day = Number(match[1]);
    const month = Number(match[2]);
    const year = Number(match[3]);
    const date = new Date(Date.UTC(year, month - 1, day));
    if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return '';
    return date.toISOString().slice(0, 10);
  };

  const growthTimestampDisplay = (milliseconds, fallbackISO) => {
    if (!(milliseconds > 0)) return growthDayDisplay(fallbackISO);
    const date = new Date(milliseconds);
    if (Number.isNaN(date.getTime())) return growthDayDisplay(fallbackISO);
    const pad = (value) => String(value).padStart(2, '0');
    return `${pad(date.getDate())}.${pad(date.getMonth() + 1)}.${date.getFullYear()}, ` +
      `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
  };

  const shiftGrowthDay = (iso, amount) => {
    const [year, month, day] = iso.split('-').map(Number);
    const date = new Date(Date.UTC(year, month - 1, day + amount));
    return date.toISOString().slice(0, 10);
  };

  const growthMonthRange = (iso, previous) => {
    const [year, month] = iso.split('-').map(Number);
    const first = new Date(Date.UTC(year, month - 1 - previous, 1));
    const after = new Date(Date.UTC(year, month - previous, 1));
    const last = new Date(after.getTime() - 86400000);
    return [first.toISOString().slice(0, 10), last.toISOString().slice(0, 10)];
  };

  const addGrowthCell = (row, value, className) => {
    const cell = document.createElement('td');
    cell.textContent = value;
    if (className) cell.className = className;
    row.appendChild(cell);
  };

  const growthRussianCount = (value, singular, paucal, plural) => {
    const count = Math.max(0, Math.trunc(Number(value) || 0));
    const mod10 = count % 10;
    const mod100 = count % 100;
    const form = mod10 === 1 && mod100 !== 11
      ? singular
      : mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) ? paucal : plural;
    return `${count.toLocaleString('ru-RU')} ${form}`;
  };

  const growthAxisMaximum = (rawMaximum) => {
    const maximum = Math.max(1, Number(rawMaximum) || 0);
    const unit = maximum >= 1024 ** 3 ? 1024 ** 3 : maximum >= 1024 ** 2 ? 1024 ** 2 : maximum >= 1024 ? 1024 : 1;
    const rawStep = maximum / unit / 4;
    const magnitude = 10 ** Math.floor(Math.log10(rawStep));
    const normalized = rawStep / magnitude;
    const nice = [1, 2, 2.5, 5, 10].find((candidate) => candidate >= normalized) || 10;
    return nice * magnitude * unit * 4;
  };

  const growthTickIndices = (length, maximumTicks) => {
    if (length <= 1) return [0];
    const count = Math.min(length, maximumTicks);
    const indices = [];
    for (let tick = 0; tick < count; tick += 1) {
      const index = Math.round(tick * (length - 1) / (count - 1));
      if (indices[indices.length - 1] !== index) indices.push(index);
    }
    return indices;
  };

  const growthChartState = new WeakMap();

  const fillGrowthTooltip = (tooltip, point) => {
    tooltip.replaceChildren();
    const title = document.createElement('div');
    title.className = 'log-growth-tooltip-title';
    title.textContent = point.title;
    tooltip.appendChild(title);
    point.lines.forEach((line) => {
      const row = document.createElement('div');
      row.className = line.danger ? 'log-growth-tooltip-row is-danger' : 'log-growth-tooltip-row';
      const label = document.createElement('span');
      label.textContent = line.label;
      const value = document.createElement('strong');
      value.textContent = line.value;
      row.append(label, value);
      tooltip.appendChild(row);
    });
  };

  const paintGrowthChart = (canvas, chart) => {
    const { width, height, ratio, series, options } = chart;
    const context = canvas.getContext('2d');
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    context.clearRect(0, 0, width, height);
    context.font = '11px system-ui, sans-serif';
    context.fillStyle = '#94a3b8';
    if (!series.length) {
      context.fillText('Нет данных', 18, 28);
      chart.tooltip.hidden = true;
      return;
    }

    const left = width < 520 ? 76 : 88;
    const right = 18;
    const top = 22;
    const bottom = 62;
    const plotWidth = Math.max(1, width - left - right);
    const plotHeight = Math.max(1, height - top - bottom);
    let rawMaximum = 1;
    series.forEach((point) => {
      rawMaximum = Math.max(rawMaximum, Number(point.value) || 0, Number(point.limit) || 0);
    });
    const maximum = growthAxisMaximum(rawMaximum);
    const x = (index) => left + (series.length === 1 ? plotWidth / 2 : index * plotWidth / (series.length - 1));
    const y = (value) => top + plotHeight - Math.max(0, Number(value) || 0) * plotHeight / maximum;
    chart.layout = { left, right, top, plotWidth, plotHeight, x, y };

    context.lineWidth = 1;
    context.textBaseline = 'middle';
    context.textAlign = 'right';
    for (let tick = 0; tick <= 4; tick += 1) {
      const value = maximum * tick / 4;
      const lineY = y(value);
      context.strokeStyle = tick === 0 ? 'rgba(148,163,184,0.34)' : 'rgba(148,163,184,0.18)';
      context.beginPath();
      context.moveTo(left, lineY);
      context.lineTo(width - right, lineY);
      context.stroke();
      context.fillStyle = '#94a3b8';
      context.fillText(formatGrowthBytes(value), left - 10, lineY);
    }

    const xTicks = growthTickIndices(series.length, width < 520 ? 2 : width < 760 ? 3 : 5);
    context.textBaseline = 'top';
    xTicks.forEach((index, position) => {
      const pointX = x(index);
      context.strokeStyle = 'rgba(148,163,184,0.12)';
      context.beginPath();
      context.moveTo(pointX, top);
      context.lineTo(pointX, top + plotHeight);
      context.stroke();
      context.fillStyle = '#94a3b8';
      context.textAlign = position === 0 ? 'left' : position === xTicks.length - 1 ? 'right' : 'center';
      context.fillText(series[index].axisLabel, pointX, top + plotHeight + 10);
    });

    context.save();
    context.translate(14, top + plotHeight / 2);
    context.rotate(-Math.PI / 2);
    context.textAlign = 'center';
    context.textBaseline = 'top';
    context.fillStyle = '#cbd5e1';
    context.font = '600 11px system-ui, sans-serif';
    context.fillText(options.yTitle, 0, 0);
    context.restore();
    context.textAlign = 'center';
    context.textBaseline = 'bottom';
    context.fillText(options.xTitle, left + plotWidth / 2, height - 2);

    if (chart.hovered >= 0 && chart.hovered < series.length) {
      const hoveredX = x(chart.hovered);
      context.save();
      context.setLineDash([3, 4]);
      context.strokeStyle = 'rgba(226,232,240,0.5)';
      context.beginPath();
      context.moveTo(hoveredX, top);
      context.lineTo(hoveredX, top + plotHeight);
      context.stroke();
      context.restore();
    }

    if (options.showLimit) {
      context.save();
      context.setLineDash([7, 5]);
      context.strokeStyle = '#fbbf24';
      context.lineWidth = 1.5;
      context.beginPath();
      series.forEach((point, index) => {
        const pointX = x(index);
        const pointY = y(point.limit);
        if (index === 0) context.moveTo(pointX, pointY);
        else context.lineTo(pointX, pointY);
      });
      context.stroke();
      context.restore();
    }

    context.strokeStyle = options.color;
    context.lineWidth = 2.2;
    context.lineJoin = 'round';
    context.lineCap = 'round';
    context.beginPath();
    series.forEach((point, index) => {
      const pointX = x(index);
      const pointY = y(point.value);
      if (index === 0) context.moveTo(pointX, pointY);
      else context.lineTo(pointX, pointY);
    });
    context.stroke();
    series.forEach((point, index) => {
      const hovered = index === chart.hovered;
      context.beginPath();
      context.fillStyle = point.limitReached ? '#ef4444' : options.color;
      context.arc(x(index), y(point.value), hovered ? 6 : point.limitReached ? 4.5 : 3, 0, Math.PI * 2);
      context.fill();
      if (hovered) {
        context.lineWidth = 2;
        context.strokeStyle = '#f8fafc';
        context.stroke();
      }
    });

    if (chart.hovered < 0 || chart.hovered >= series.length) {
      chart.tooltip.hidden = true;
      canvas.setAttribute('aria-label', options.ariaLabel);
      return;
    }
    const point = series[chart.hovered];
    const pointX = x(chart.hovered);
    const pointY = y(point.value);
    fillGrowthTooltip(chart.tooltip, point);
    chart.tooltip.hidden = false;
    const desiredLeft = canvas.offsetLeft + pointX - chart.tooltip.offsetWidth / 2;
    const maximumLeft = canvas.parentElement.clientWidth - chart.tooltip.offsetWidth - 6;
    chart.tooltip.style.left = `${Math.max(6, Math.min(maximumLeft, desiredLeft))}px`;
    chart.tooltip.style.top = `${canvas.offsetTop + pointY}px`;
    chart.tooltip.classList.toggle('is-below', pointY < 112);
    canvas.setAttribute('aria-label', `${options.ariaLabel}. ${point.title}. ${point.lines.map((line) => `${line.label}: ${line.value}`).join('. ')}`);
  };

  const drawGrowthChart = (canvas, series, options) => {
    if (!canvas) return;
    let chart = growthChartState.get(canvas);
    if (!chart) {
      const tooltip = document.createElement('div');
      tooltip.className = 'log-growth-tooltip';
      tooltip.setAttribute('role', 'tooltip');
      tooltip.hidden = true;
      canvas.parentNode.appendChild(tooltip);
      chart = { hovered: -1, layout: null, options, ratio: 1, series, tooltip, width: 0, height: 0 };
      growthChartState.set(canvas, chart);
      const selectPoint = (event) => {
        if (!chart.layout || !chart.series.length) return;
        const bounds = canvas.getBoundingClientRect();
        const pointerX = event.clientX - bounds.left;
        const pointerY = event.clientY - bounds.top;
        const { left, top, plotWidth, plotHeight } = chart.layout;
        if (pointerX < left || pointerX > left + plotWidth || pointerY < top || pointerY > top + plotHeight) {
          if (chart.hovered !== -1) {
            chart.hovered = -1;
            paintGrowthChart(canvas, chart);
          }
          return;
        }
        const next = chart.series.length === 1
          ? 0
          : Math.max(0, Math.min(chart.series.length - 1, Math.round((pointerX - left) * (chart.series.length - 1) / plotWidth)));
        if (next !== chart.hovered) {
          chart.hovered = next;
          paintGrowthChart(canvas, chart);
        }
      };
      canvas.addEventListener('pointermove', selectPoint, { passive: true });
      canvas.addEventListener('pointerdown', selectPoint, { passive: true });
      canvas.addEventListener('pointerleave', () => {
        if (chart.hovered !== -1 && document.activeElement !== canvas) {
          chart.hovered = -1;
          paintGrowthChart(canvas, chart);
        }
      }, { passive: true });
      canvas.addEventListener('focus', () => {
        if (chart.hovered < 0 && chart.series.length) {
          chart.hovered = 0;
          paintGrowthChart(canvas, chart);
        }
      });
      canvas.addEventListener('blur', () => {
        chart.hovered = -1;
        paintGrowthChart(canvas, chart);
      });
      canvas.addEventListener('keydown', (event) => {
        if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
        event.preventDefault();
        const direction = event.key === 'ArrowRight' ? 1 : -1;
        chart.hovered = Math.max(0, Math.min(chart.series.length - 1, Math.max(0, chart.hovered) + direction));
        paintGrowthChart(canvas, chart);
      });
    }
    chart.series = series;
    chart.options = options;
    chart.width = Math.max(1, Math.floor(canvas.clientWidth || 640));
    chart.height = Math.max(1, Math.floor(canvas.clientHeight || 280));
    chart.ratio = Math.min(window.devicePixelRatio || 1, 2);
    canvas.width = Math.floor(chart.width * chart.ratio);
    canvas.height = Math.floor(chart.height * chart.ratio);
    if (chart.hovered >= series.length) chart.hovered = -1;
    paintGrowthChart(canvas, chart);
  };

  const prepareGrowthTables = (panel) => {
    panel.querySelectorAll('.log-growth-table').forEach((table) => {
      if (table.closest('.table-scroll')) return;
      const wrapper = document.createElement('div');
      wrapper.className = 'table-scroll';
      table.parentNode.insertBefore(wrapper, table);
      wrapper.appendChild(table);
    });
    requestAnimationFrame(() => measureGrowthTables(panel));
  };

  const measureGrowthTables = (panel) => {
    panel.querySelectorAll('.table-scroll').forEach((wrapper) => {
      wrapper.classList.toggle('is-scrollable', wrapper.scrollWidth > wrapper.clientWidth + 4);
    });
  };

  const renderGrowthRowPages = (tbody, items, columns, renderRow, panel, reverse = false) => {
    let loaded = 0;
    let loader = null;
    const appendPage = () => {
      const end = Math.min(items.length, loaded + 50);
      const fragment = document.createDocumentFragment();
      while (loaded < end) {
        const index = reverse ? items.length - loaded - 1 : loaded;
        fragment.appendChild(renderRow(items[index]));
        loaded += 1;
      }
      if (loader) loader.before(fragment);
      else tbody.appendChild(fragment);
      const remaining = items.length - loaded;
      if (!remaining) {
        loader?.remove();
        loader = null;
      } else if (!loader) {
        loader = document.createElement('tr');
        loader.className = 'deferred-table-loader';
        const cell = document.createElement('td');
        cell.colSpan = columns;
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'deferred-table-button';
        const remainingLabel = document.createElement('span');
        remainingLabel.dataset.deferredRemaining = '';
        cell.append(button, remainingLabel);
        loader.appendChild(cell);
        tbody.appendChild(loader);
        button.addEventListener('click', appendPage);
      }
      if (loader) {
        loader.querySelector('button').textContent = `Показать ещё ${Math.min(50, remaining)}`;
        loader.querySelector('[data-deferred-remaining]').textContent = `Осталось строк: ${remaining}`;
      }
      requestAnimationFrame(() => measureGrowthTables(panel));
    };
    appendPage();
  };

  document.querySelectorAll('[data-log-growth]').forEach((panel) => {
    const source = panel.querySelector('[data-log-growth-json]');
    if (!source) return;
    let data;
    try {
      data = JSON.parse(source.textContent);
    } catch (_) {
      return;
    }
    const sessions = Array.isArray(data.sessions) ? data.sessions : [];
    const days = Array.isArray(data.days) ? data.days : [];
    // Tables retain every available record. Charts deliberately keep a bounded point window:
    // beyond it points overlap and canvas work grows without adding readable information.
    const chartSessions = sessions.slice(-256);
    const chartDays = days.slice(-400);
    const chartSessionOffset = sessions.length - chartSessions.length;
    const details = panel.querySelector('details');
    const fromInput = panel.querySelector('[data-growth-from]');
    const toInput = panel.querySelector('[data-growth-to]');
    const periodResult = panel.querySelector('[data-growth-period-result]');
    const periodNotice = panel.querySelector('[data-growth-period-empty]');
    const periodInsight = panel.querySelector('[data-growth-period-insight]');
    const insightTitle = panel.querySelector('[data-growth-insight-title]');
    const insightSummary = panel.querySelector('[data-growth-insight-summary]');
    const insightDetails = panel.querySelector('[data-growth-insight-details]');
    const valueNodes = new Map(
      Array.from(panel.querySelectorAll('[data-growth-value]')).map((node) => [node.dataset.growthValue, node]),
    );
    let initialized = false;

    const renderTables = () => {
      const sessionBody = panel.querySelector('[data-growth-session-rows]');
      renderGrowthRowPages(sessionBody, sessions, 7, (session) => {
        const row = document.createElement('tr');
        if (Number(session.limit_reached_count) > 0) row.className = 'has-limit';
        const started = Number(session.started_at_ms) || 0;
        const ended = Math.max(started, Number(session.ended_at_ms) || started);
        const duration = ended - started;
        const speed = duration > 0 ? Number(session.generated_bytes) * 1000 / duration : 0;
        const date = growthTimestampDisplay(started, growthDayISO(session.day_key));
        addGrowthCell(row, session.completed ? date : `${date} · идёт`);
        addGrowthCell(row, formatGrowthDuration(duration));
        addGrowthCell(row, formatGrowthBytes(session.maximum_retained_bytes));
        addGrowthCell(row, formatGrowthBytes(session.generated_bytes));
        addGrowthCell(row, speed > 0 ? `${formatGrowthBytes(speed)}/с` : '—');
        addGrowthCell(row, formatGrowthBytes(session.configured_limit_bytes));
        addGrowthCell(row, String(session.segment_rotation_count || 0));
        addGrowthCell(row, Number(session.limit_reached_count) > 0 ? 'да' : 'нет', Number(session.limit_reached_count) > 0 ? 'log-growth-limit' : '');
        addGrowthCell(row, formatGrowthBytes(session.archive_evicted_bytes));
        return row;
      }, panel, true);

      const dayBody = panel.querySelector('[data-growth-day-rows]');
      renderGrowthRowPages(dayBody, days, 7, (day) => {
        const row = document.createElement('tr');
        if (Number(day.limit_reached_count) > 0) row.className = 'has-limit';
        addGrowthCell(row, growthDayDisplay(growthDayISO(day.day_key)));
        addGrowthCell(row, String(day.session_count || 0));
        addGrowthCell(row, formatGrowthDuration(day.total_duration_ms));
        addGrowthCell(row, formatGrowthBytes(day.generated_bytes));
        addGrowthCell(row, formatGrowthBytes(day.maximum_retained_bytes));
        addGrowthCell(row, `${((Number(day.maximum_fill_permille) || 0) / 10).toLocaleString('ru-RU', { maximumFractionDigits: 1 })}%`);
        addGrowthCell(row, String(day.segment_rotation_count || 0));
        addGrowthCell(row, String(day.limit_reached_count || 0), Number(day.limit_reached_count) > 0 ? 'log-growth-limit' : '');
        addGrowthCell(row, formatGrowthBytes(day.archive_evicted_bytes));
        return row;
      }, panel, true);
      prepareGrowthTables(panel);
    };

    const renderCharts = () => {
      drawGrowthChart(
        panel.querySelector('[data-growth-session-chart]'),
        chartSessions.map((session, index) => ({
          axisLabel: growthTimestampDisplay(
            Number(session.started_at_ms) || 0,
            growthDayISO(session.day_key),
          ).split(',')[0],
          value: Number(session.maximum_retained_bytes) || 0,
          limit: Number(session.configured_limit_bytes) || 0,
          limitReached: Number(session.limit_reached_count) > 0,
          title: `Сессия №${chartSessionOffset + index + 1} · ${growthTimestampDisplay(Number(session.started_at_ms) || 0, growthDayISO(session.day_key))}`,
          lines: [
            { label: 'Максимальный размер', value: formatGrowthBytes(session.maximum_retained_bytes) },
            { label: 'Выставленный лимит', value: Number(session.configured_limit_bytes) > 0 ? formatGrowthBytes(session.configured_limit_bytes) : 'не задан' },
            { label: 'Создано за сессию', value: formatGrowthBytes(session.generated_bytes) },
            {
              label: 'Общий лимит',
              value: Number(session.limit_reached_count) > 0 ? 'исчерпан' : 'не исчерпан',
              danger: Number(session.limit_reached_count) > 0,
            },
            { label: 'Ротации сегментов', value: String(session.segment_rotation_count || 0) },
            { label: 'Удалено из архива', value: formatGrowthBytes(session.archive_evicted_bytes) },
          ],
        })),
        {
          ariaLabel: 'Максимальный объём журналов в каждом запуске. Красные точки обозначают исчерпание общего лимита.',
          color: '#38bdf8',
          showLimit: true,
          xTitle: 'Дата начала сессии',
          yTitle: 'Максимальный размер',
        },
      );
      drawGrowthChart(
        panel.querySelector('[data-growth-day-chart]'),
        chartDays.map((day) => ({
          axisLabel: growthDayDisplay(growthDayISO(day.day_key)),
          value: Number(day.generated_bytes) || 0,
          limitReached: Number(day.limit_reached_count) > 0,
          title: `День · ${growthDayDisplay(growthDayISO(day.day_key))}`,
          lines: [
            { label: 'Создано за день', value: formatGrowthBytes(day.generated_bytes) },
            { label: 'Сессии', value: growthRussianCount(day.session_count, 'сессия', 'сессии', 'сессий') },
            { label: 'Максимум за запуск', value: formatGrowthBytes(day.maximum_retained_bytes) },
            {
              label: 'Исчерпания общего лимита',
              value: String(day.limit_reached_count || 0),
              danger: Number(day.limit_reached_count) > 0,
            },
            { label: 'Ротации сегментов', value: String(day.segment_rotation_count || 0) },
            { label: 'Удалено из архива', value: formatGrowthBytes(day.archive_evicted_bytes) },
          ],
        })),
        {
          ariaLabel: 'Объём данных, созданных за каждый день. Красные точки обозначают дни с остановкой по общему лимиту.',
          color: '#34d399',
          showLimit: false,
          xTitle: 'Дата',
          yTitle: 'Создано за день',
        },
      );
    };

    const initialize = () => {
      if (!initialized) {
        initialized = true;
        renderTables();
      }
      renderCharts();
    };

    const setPeriod = (kind) => {
      const latest = days.length ? growthDayISO(days[days.length - 1].day_key) : new Date().toISOString().slice(0, 10);
      let range;
      if (kind === 'week') range = [shiftGrowthDay(latest, -6), latest];
      if (kind === 'month') range = [shiftGrowthDay(latest, -29), latest];
      if (kind === 'current-month') range = growthMonthRange(latest, 0);
      if (kind === 'previous-month') range = growthMonthRange(latest, 1);
      if (!range) return;
      fromInput.value = growthDayDisplay(range[0]);
      toInput.value = growthDayDisplay(range[1]);
      panel.querySelectorAll('[data-growth-period]').forEach((button) => {
        button.classList.toggle('is-active', button.dataset.growthPeriod === kind);
      });
    };

    panel.querySelectorAll('[data-growth-period]').forEach((button) => {
      button.addEventListener('click', () => setPeriod(button.dataset.growthPeriod));
    });
    [fromInput, toInput].forEach((input) => input.addEventListener('input', () => {
      panel.querySelectorAll('[data-growth-period]').forEach((button) => button.classList.remove('is-active'));
    }));

    panel.querySelector('[data-growth-calculate]').addEventListener('click', () => {
      const from = growthDisplayDayISO(fromInput.value);
      const to = growthDisplayDayISO(toInput.value);
      const selected = days.filter((day) => {
        const iso = growthDayISO(day.day_key);
        return iso && from && to && iso >= from && iso <= to;
      });
      if (!from || !to || from > to || !selected.length) {
        periodResult.hidden = true;
        periodInsight.hidden = true;
        periodNotice.hidden = false;
        periodNotice.textContent = !from || !to || from > to
          ? 'Укажите правильные начальную и конечную даты.'
          : 'За выбранные дни данных нет.';
        return;
      }
      const totals = selected.reduce((result, day) => ({
        sessions: result.sessions + (Number(day.session_count) || 0),
        duration: result.duration + (Number(day.total_duration_ms) || 0),
        generated: result.generated + (Number(day.generated_bytes) || 0),
        retained: Math.max(result.retained, Number(day.maximum_retained_bytes) || 0),
        fill: Math.max(result.fill, Number(day.maximum_fill_permille) || 0),
        reached: result.reached + (Number(day.sessions_reaching_limit) || 0),
        limitReached: result.limitReached + (Number(day.limit_reached_count) || 0),
        rotations: result.rotations + (Number(day.segment_rotation_count) || 0),
        evicted: result.evicted + (Number(day.archive_evicted_bytes) || 0),
      }), { sessions: 0, duration: 0, generated: 0, retained: 0, fill: 0, reached: 0, limitReached: 0, rotations: 0, evicted: 0 });
      valueNodes.get('sessions').textContent = totals.sessions.toLocaleString('ru-RU');
      valueNodes.get('duration').textContent = formatGrowthDuration(totals.duration);
      valueNodes.get('generated').textContent = formatGrowthBytes(totals.generated);
      valueNodes.get('retained').textContent = formatGrowthBytes(totals.retained);
      valueNodes.get('fill').textContent = `${(totals.fill / 10).toLocaleString('ru-RU', { maximumFractionDigits: 1 })}% лимита`;
      valueNodes.get('reached').textContent = `${totals.reached.toLocaleString('ru-RU')} сессий достигли лимита`;
      valueNodes.get('limit-reached').textContent = totals.limitReached.toLocaleString('ru-RU');
      valueNodes.get('rotations').textContent = totals.rotations.toLocaleString('ru-RU');
      valueNodes.get('evicted').textContent = formatGrowthBytes(totals.evicted);
      const reachedShare = totals.sessions > 0 ? totals.reached / totals.sessions : 0;
      const fillPercent = totals.fill / 10;
      const peakDay = selected.reduce((peak, day) => (
        Number(day.generated_bytes) > Number(peak.generated_bytes) ? day : peak
      ), selected[0]);
      if (totals.limitReached === 0) {
        periodInsight.className = 'log-growth-insight is-calm';
        insightTitle.textContent = 'Общий лимит не исчерпывался';
        insightSummary.textContent = `За выбранный период сбор не останавливался по лимиту. Наибольшее заполнение — ${fillPercent.toLocaleString('ru-RU', { maximumFractionDigits: 1 })}% общего бюджета; ротаций сегментов — ${totals.rotations.toLocaleString('ru-RU')}.`;
      } else if (reachedShare >= 0.3) {
        periodInsight.className = 'log-growth-insight is-warning';
        insightTitle.textContent = 'Общий лимит часто исчерпывается';
        insightSummary.textContent = `Сбор остановился по лимиту в ${totals.reached.toLocaleString('ru-RU')} из ${totals.sessions.toLocaleString('ru-RU')} сессий. Проверьте объём полезных событий и достаточность общего бюджета.`;
      } else {
        periodInsight.className = 'log-growth-insight is-attention';
        insightTitle.textContent = 'Есть отдельные остановки по лимиту';
        insightSummary.textContent = `Сбор остановился по лимиту в ${totals.reached.toLocaleString('ru-RU')} из ${totals.sessions.toLocaleString('ru-RU')} сессий. Красные точки показывают конкретные запуски.`;
      }
      const averageRate = totals.duration > 0 ? totals.generated * 1000 / totals.duration : 0;
      insightDetails.textContent = `Средняя скорость создания данных — ${averageRate > 0 ? `${formatGrowthBytes(averageRate)}/с` : 'нет данных'}. Самый объёмный день — ${growthDayDisplay(growthDayISO(peakDay.day_key))}: ${formatGrowthBytes(peakDay.generated_bytes)}.${totals.evicted > 0 ? ` Для соблюдения бюджета удалено ${formatGrowthBytes(totals.evicted)} завершённых архивных журналов.` : ''}`;
      periodResult.hidden = false;
      periodInsight.hidden = false;
      periodNotice.hidden = true;
      measureGrowthTables(panel);
    });

    details.addEventListener('toggle', () => {
      if (details.open) requestAnimationFrame(initialize);
    });
    if (details.open) requestAnimationFrame(initialize);
    let resizeTimer;
    window.addEventListener('resize', () => {
      if (!initialized || !details.open) return;
      window.clearTimeout(resizeTimer);
      resizeTimer = window.setTimeout(renderCharts, 100);
    }, { passive: true });
  });
})();
