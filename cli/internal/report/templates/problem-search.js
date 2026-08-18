const initializeProblemSearch = (revealDeferredEntry, archivedEntries) => {
  document.querySelectorAll('[data-problem-inbox]').forEach((inbox, inboxIndex) => {
    const cards = Array.from(inbox.querySelectorAll('[data-problem-card]'));
    const search = inbox.querySelector('[data-problem-search]');
    const severity = inbox.querySelector('[data-problem-severity]');
    const category = inbox.querySelector('[data-problem-category]');
    const confidence = inbox.querySelector('[data-problem-confidence]');
    const status = inbox.querySelector('[data-problem-status]');
    const count = inbox.querySelector('[data-problem-count]');
    const empty = inbox.querySelector('[data-problem-filter-empty]');
    const feedback = inbox.querySelector('[data-problem-search-feedback]');
    const clear = inbox.querySelector('[data-problem-search-clear]');
    const searchResults = inbox.querySelector('[data-problem-search-results]');
    const cardScope = inbox.querySelector('[data-problem-card-scope]');
    const categoryButtons = Array.from(inbox.querySelectorAll('[data-problem-category-button]'));
    const normalize = (value) => (value || '')
      .normalize('NFKC')
      .trim()
      .toLocaleLowerCase('ru')
      .replaceAll('ё', 'е');
    const searchableText = new Map(cards.map((card) => [card, normalize(card.textContent)]));
    const detailSelector = [
      'main section[id] tr',
      'main section[id] .metric',
      'main section[id] .finding',
      'main section[id] .section-overview-card',
      'main section[id] .analysis-insight-card',
    ].join(', ');
    const detailIndex = [];
    const indexedDetailNodes = new WeakSet();
    const deferredEntries = new Map();
    let detailSequence = 0;
    let deferredIndexReady = false;
    let deferredIndexPromise = null;
    let deferredIndexIncomplete = false;
    let applyGeneration = 0;

    const createDetailEntry = (node, section) => {
      const sectionTitle = section?.querySelector('h2')?.textContent?.trim() || 'Подробности отчёта';
      const primaryElement = node.querySelector('code') ||
        node.querySelector('td') ||
        node.querySelector('strong') ||
        node.querySelector('.value');
      const primary = primaryElement?.textContent?.trim();
      const fullText = node.textContent?.trim().replace(/\s+/g, ' ') || '';
      if (!fullText) return null;
      detailSequence += 1;
      return {
        node,
        sectionTitle,
        label: primary || fullText.slice(0, 140),
        text: normalize(fullText),
        id: `report-search-match-${inboxIndex + 1}-${detailSequence}`,
      };
    };

    const addLiveDetailNode = (node) => {
      if (!node || node.closest('[data-problem-inbox]') || indexedDetailNodes.has(node)) return;
      const searchID = node.dataset.reportSearchId;
      const deferredEntry = searchID ? deferredEntries.get(searchID) : null;
      if (deferredEntry) {
        deferredEntry.node = node;
        indexedDetailNodes.add(node);
        return;
      }
      const entry = createDetailEntry(node, node.closest('section[id]'));
      if (!entry) return;
      detailIndex.push(entry);
      indexedDetailNodes.add(node);
    };

    document.querySelectorAll(detailSelector).forEach(addLiveDetailNode);

    const deferredScripts = Array.from(document.querySelectorAll(
      'main section[id] script[data-table-chunk]'
    )).filter((script) => !script.closest('[data-problem-inbox]'));

    const indexDeferredScript = (script) => {
      if (!script.isConnected) return;
      const markup = JSON.parse(script.textContent || '""');
      const stagingTable = document.createElement('table');
      const stagingBody = stagingTable.createTBody();
      stagingBody.innerHTML = markup;
      const section = script.closest('section[id]');
      const deferredBody = script.closest('tbody');
      Array.from(stagingBody.rows).forEach((row) => {
        const entry = createDetailEntry(row, section);
        if (!entry) return;
        const searchID = `report-search-deferred-${inboxIndex + 1}-${detailSequence}`;
        row.dataset.reportSearchId = searchID;
        entry.node = null;
        entry.deferredBody = deferredBody;
        entry.deferredScript = script;
        entry.searchID = searchID;
        deferredEntries.set(searchID, entry);
        detailIndex.push(entry);
      });
      script.textContent = JSON.stringify(stagingBody.innerHTML)
        .replaceAll('<', '\\u003c')
        .replaceAll('>', '\\u003e')
        .replaceAll('&', '\\u0026');
    };

    const ensureDeferredDetailsIndexed = () => {
      if (deferredIndexReady) return Promise.resolve();
      if (deferredIndexPromise) return deferredIndexPromise;
      deferredIndexPromise = new Promise((resolve) => {
        let index = 0;
        const finish = async () => {
          try {
            const entries = await archivedEntries();
            entries.forEach((entry) => {
              detailSequence += 1;
              detailIndex.push({
                ...entry,
                node: null,
                text: normalize(entry.text),
                id: `report-search-match-${inboxIndex + 1}-${detailSequence}`,
              });
            });
          } catch (error) {
            deferredIndexIncomplete = true;
            console.error('Не удалось добавить архив реестра кода в индекс поиска', error);
          }
          deferredIndexReady = true;
          resolve();
        };
        const step = () => {
          const started = performance.now();
          while (index < deferredScripts.length && performance.now() - started < 8) {
            try {
              indexDeferredScript(deferredScripts[index]);
            } catch (error) {
              deferredIndexIncomplete = true;
              console.error('Не удалось добавить отложенные строки в индекс поиска', error);
            }
            index += 1;
          }
          if (index < deferredScripts.length) {
            requestAnimationFrame(step);
            return;
          }
          void finish();
        };
        requestAnimationFrame(step);
      });
      return deferredIndexPromise;
    };

    const openAndReveal = async (entry) => {
      const liveNode = entry.node?.isConnected ? entry.node : null;
      const node = liveNode || await (entry.reveal ? entry.reveal() : revealDeferredEntry(entry));
      if (!node) return;
      entry.node = node;
      let details = node.closest('details');
      while (details) {
        details.open = true;
        details = details.parentElement?.closest('details');
      }
      if (node.hidden) node.hidden = false;
      if (!node.id) node.id = entry.id;
      node.classList.add('report-search-highlight');
      node.scrollIntoView({ behavior: 'smooth', block: 'center' });
      window.setTimeout(() => node.classList.remove('report-search-highlight'), 2600);
    };

    const renderDetailMatches = (matches, query) => {
      if (!searchResults) return;
      searchResults.replaceChildren();
      searchResults.hidden = !query || matches.length === 0;
      if (searchResults.hidden) return;
      const heading = document.createElement('strong');
      heading.textContent = `Совпадения в подробных разделах: ${matches.length}`;
      const list = document.createElement('div');
      matches.slice(0, 8).forEach((entry) => {
        const button = document.createElement('button');
        button.type = 'button';
        button.textContent = `${entry.sectionTitle}: ${entry.label}`;
        button.addEventListener('click', () => { void openAndReveal(entry); });
        list.appendChild(button);
      });
      searchResults.append(heading, list);
      if (matches.length > 8) {
        const note = document.createElement('small');
        note.textContent =
          'Показаны первые 8 совпадений. Уточните запрос, чтобы сузить результат.';
        searchResults.appendChild(note);
      }
    };

    const apply = () => {
      const generation = ++applyGeneration;
      const query = normalize(search?.value);
      const queryParts = query.split(/\s+/).filter(Boolean);
      if (queryParts.length && !deferredIndexReady) {
        if (clear) clear.hidden = false;
        if (feedback) {
          feedback.classList.remove('has-result', 'has-no-result');
          feedback.textContent =
            'Ищу по всем строкам подробных разделов, включая ещё не раскрытые…';
        }
        void ensureDeferredDetailsIndexed().then(() => {
          if (generation === applyGeneration) apply();
        });
        return;
      }
      const detailMatches = queryParts.length
        ? detailIndex.filter((entry) => queryParts.every((part) => entry.text.includes(part)))
        : [];
      let visible = 0;
      cards.forEach((card) => {
        const text = searchableText.get(card);
        const matches =
          (!queryParts.length || queryParts.every((part) => text.includes(part))) &&
          (!severity?.value || card.dataset.severity === severity.value) &&
          (!category?.value || (card.dataset.category || '').split(/\s+/).includes(category.value)) &&
          (!confidence?.value || card.dataset.confidence === confidence.value) &&
          (!status?.value || card.dataset.status === status.value);
        card.hidden = !matches;
        if (matches) visible += 1;
      });
      if (count) count.textContent = queryParts.length
        ? `Карточки проблем: ${visible} · подробности: ${detailMatches.length}`
        : severity?.value || category?.value || confidence?.value || status?.value
          ? `Найдено: ${visible} из ${cards.length}`
        : `Карточек проблем: ${cards.length}`;
      if (empty) {
        const emptyTitle = empty.querySelector('strong');
        const emptyHint = empty.querySelector('p');
        const hasDetailMatches = queryParts.length > 0 && detailMatches.length > 0;
        if (emptyTitle) {
          emptyTitle.textContent = hasDetailMatches
            ? 'Среди карточек проблем совпадений нет.'
            : 'По выбранным условиям карточек проблем нет.';
        }
        if (emptyHint) {
          emptyHint.textContent = hasDetailMatches
            ? 'Совпадения в классах и подробных строках показаны выше.'
            : 'Сбросьте поиск или фильтры либо выберите другую категорию.';
        }
        empty.hidden = visible !== 0 || cards.length === 0;
      }
      if (clear) clear.hidden = !query;
      if (feedback) {
        const totalMatches = visible + detailMatches.length;
        const incompleteNote = deferredIndexIncomplete
          ? ' Часть повреждённых отложенных строк не вошла в поиск.'
          : '';
        feedback.classList.toggle('has-result', Boolean(query) && totalMatches > 0);
        feedback.classList.toggle('has-no-result', Boolean(query) && totalMatches === 0);
        if (!query) {
          feedback.textContent =
            'Поиск проверяет карточки и все строки подробных разделов этой страницы. ' +
            'Фильтры справа изменяют только карточки проблем.';
        } else if (totalMatches > 0) {
          feedback.textContent =
            `По запросу «${search.value.trim()}»: карточек проблем — ${visible}, ` +
            `совпадений в подробностях — ${detailMatches.length}.${incompleteNote}`;
        } else {
          feedback.textContent =
            `В отчёте нет совпадений с «${search.value.trim()}». ` +
            `Проверьте написание или сократите запрос.${incompleteNote}`;
        }
      }
      renderDetailMatches(detailMatches, query);
      categoryButtons.forEach((button) => {
        button.setAttribute('aria-pressed', String(Boolean(category?.value) && category.value === button.dataset.problemCategoryButton));
      });
    };
    [search, severity, category, confidence, status].forEach((control) => {
      control?.addEventListener(control === search ? 'input' : 'change', apply);
    });
    if (cardScope) {
      cardScope.textContent =
        `Карточки проблем: ${cards.length}. ` +
        'Поиск также охватывает все классы и строки подробностей';
    }
    categoryButtons.forEach((button) => {
      button.addEventListener('click', () => {
        if (!category) return;
        const selected = button.dataset.problemCategoryButton || '';
        category.value = category.value === selected ? '' : selected;
        apply();
      });
    });
    clear?.addEventListener('click', () => {
      if (!search) return;
      search.value = '';
      search.focus();
      apply();
    });
    document.addEventListener('report:rows-added', (event) => {
      (event.detail?.rows || []).forEach(addLiveDetailNode);
    });
    search?.addEventListener('keydown', (event) => {
      if (event.key !== 'Escape' || !search.value) return;
      search.value = '';
      apply();
    });
    apply();
  });
};
