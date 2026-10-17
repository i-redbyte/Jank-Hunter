(() => {
  const root = document.querySelector('[data-worker-catalog]');
  if (!root) return;
  const data = JSON.parse(root.querySelector('script[type="application/json"]').textContent);
  const select = root.querySelector('select');
  const search = root.querySelector('input');
  const body = root.querySelector('tbody');
  const info = root.querySelector('[data-catalog-info]');
  const pageLabel = root.querySelector('[data-catalog-page]');
  let page = 0;
  (data.Observations || []).forEach((o, index) => {
    const option = document.createElement('option');
    option.value = index;
    option.textContent = `${o.Process || 'процесс неизвестен'} · журнал ${o.LogIndex} · ${o.TimeMS} мс`;
    select.append(option);
  });
  const draw = () => {
    const observation = (data.Observations || [])[Number(select.value)];
    if (!observation) return;
    const c = observation.Catalog;
    const statuses = {missing:'Каталог не упакован в приложение',unreadable:'Каталог не удалось прочитать',oversized:'Каталог превышает лимит размера'};
    info.textContent = c.status === 'available'
      ? `Обнаружено классов: ${(c.classes || []).length}. Не разрешены иерархии: ${c.unresolved_classes || 0}. Пропущено по лимиту: ${c.omitted_workers || 0}. Пространство сборки: ${c.namespace}.`
      : (statuses[c.status] || 'Каталог недоступен');
    const rows = (c.classes || []).filter(name => name.toLowerCase().includes(search.value.toLowerCase()));
    const pages = Math.max(1, Math.ceil(rows.length / 100));
    page = Math.min(page, pages - 1);
    body.replaceChildren();
    rows.slice(page * 100, (page + 1) * 100).forEach(name => {
      const tr = document.createElement('tr');
      const td = document.createElement('td');
      td.textContent = name;
      tr.append(td);
      body.append(tr);
    });
    pageLabel.textContent = `Страница ${page + 1} из ${pages}; найдено ${rows.length}. Пропущено документов в анализе: ${data.DroppedDocuments || 0}.`;
  };
  select.addEventListener('change', () => { page = 0; draw(); });
  search.addEventListener('input', () => { page = 0; draw(); });
  root.querySelector('[data-catalog-prev]').addEventListener('click', () => { page = Math.max(0, page - 1); draw(); });
  root.querySelector('[data-catalog-next]').addEventListener('click', () => { page++; draw(); });
  draw();
})();
