import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const cliRoot = resolve(scriptDirectory, "..");
const temporaryDirectory = mkdtempSync(resolve(tmpdir(), "jh-log-growth-report-"));
const reportPath = resolve(temporaryDirectory, "inspect.html");
const paginationReportPath = resolve(temporaryDirectory, "inspect-pagination.html");
const goCachePath = resolve(temporaryDirectory, "go-cache");

const equal = (actual, expected, label) => {
  if (actual !== expected) {
    throw new Error(`${label}: получено ${JSON.stringify(actual)}, ожидалось ${JSON.stringify(expected)}`);
  }
};

const buildReport = () => {
  const result = spawnSync(
    "go",
    ["test", "./internal/report", "-run", "^TestWriteLogGrowth(Visual|Pagination)Fixture$", "-count=1"],
    {
      cwd: cliRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        GOCACHE: goCachePath,
        JH_GROWTH_VISUAL_OUT: reportPath,
        JH_GROWTH_PAGINATION_OUT: paginationReportPath,
      },
    },
  );
  if (result.status !== 0) {
    throw new Error(`Не удалось создать проверочный отчёт\n${result.stdout}\n${result.stderr}`);
  }
};

const value = async (page, name) => page.locator(`[data-growth-value="${name}"]`).textContent();

const assertTotals = async (page, expected, label) => {
  for (const [name, expectedValue] of Object.entries(expected)) {
    equal(await value(page, name), expectedValue, `${label}, ${name}`);
  }
  equal(await page.locator("[data-growth-period-result]").isVisible(), true, `${label}, итог показан`);
  equal(await page.locator("[data-growth-period-empty]").isVisible(), false, `${label}, сообщение скрыто`);
};

const selectPreset = async (page, name, from, to) => {
  await page.locator(`[data-growth-period="${name}"]`).click();
  equal(await page.locator("[data-growth-from]").inputValue(), from, `${name}, начальная дата`);
  equal(await page.locator("[data-growth-to]").inputValue(), to, `${name}, конечная дата`);
  await page.locator("[data-growth-calculate]").click();
};

const setCustomRange = async (page, from, to) => {
  await page.locator("[data-growth-from]").fill(from);
  await page.locator("[data-growth-to]").fill(to);
  await page.locator("[data-growth-calculate]").click();
};

let browser;
try {
  buildReport();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ locale: "ru-RU" });
  await page.goto(pathToFileURL(reportPath).href);

  const details = page.locator("#log-growth details");
  equal(await details.getAttribute("open"), null, "раздел изначально свёрнут");
  await page.locator("#log-growth summary").click();
  await page.locator("[data-growth-session-rows] tr").first().waitFor();
  equal(await page.locator("[data-growth-session-rows] tr").count(), 31, "строки сессий за месяц");
  equal(await page.locator("[data-growth-day-rows] tr").count(), 31, "строки дней за месяц");
  equal(await page.locator("[data-growth-session-rows] tr.has-limit").count(), 6, "сессии с остановкой по лимиту");
  equal(await page.locator("[data-growth-day-rows] tr.has-limit").count(), 6, "дни с остановкой по лимиту");
  equal(
    await page.locator("[data-growth-session-rows] tr").first().locator("td").first().textContent(),
    "31.07.2026, 15:00:00",
    "дата последней сессии",
  );
  equal(
    await page.locator("[data-growth-day-rows] tr").first().locator("td").first().textContent(),
    "31.07.2026",
    "дата последнего дня",
  );
  equal(
    await page.locator("[data-growth-day-rows] tr").last().locator("td").first().textContent(),
    "01.07.2026",
    "дата первого дня",
  );
  equal(/\b\d{4}-\d{2}-\d{2}\b/.test(await page.locator("#log-growth").innerText()), false, "в разделе нет видимых дат внутреннего вида");
  equal(await page.locator(".log-growth-chart-help").count(), 2, "у обоих графиков есть объяснение осей");
  equal(
    (await page.locator(".log-growth-chart-help").first().textContent()).includes("по горизонтали — дата начала сессии"),
    true,
    "объяснена горизонтальная ось сессий",
  );
  equal(await page.locator(".log-growth-tooltip").count(), 2, "для обоих графиков созданы подсказки");

  const sessionCanvas = page.locator("[data-growth-session-chart]");
  await sessionCanvas.scrollIntoViewIfNeeded();
  const sessionBounds = await sessionCanvas.boundingBox();
  if (!sessionBounds) throw new Error("Не удалось определить положение графика сессий");
  const sessionPlotLeft = sessionBounds.width < 520 ? 76 : 88;
  const sessionPlotWidth = sessionBounds.width - sessionPlotLeft - 18;
  await page.mouse.move(
    sessionBounds.x + sessionPlotLeft + 4 * sessionPlotWidth / 30,
    sessionBounds.y + 24,
  );
  const sessionTooltip = page.locator(".log-growth-tooltip").first();
  equal(await sessionTooltip.isVisible(), true, "подсказка красной точки показана");
  const sessionTooltipText = await sessionTooltip.innerText();
  equal(sessionTooltipText.includes("Сессия №5 · 05.07.2026"), true, "подсказка содержит понятную дату и номер сессии");
  equal(sessionTooltipText.includes("Общий лимит\nисчерпан"), true, "подсказка объясняет красную точку");
  equal(sessionTooltipText.includes("Ротации сегментов\n2"), true, "подсказка показывает lossless-ротации");
  equal(sessionTooltipText.includes("Удалено из архива\n1 МиБ"), true, "подсказка показывает архивную очистку");
  await sessionCanvas.focus();
  await sessionCanvas.press("ArrowRight");
  equal(
    (await sessionTooltip.innerText()).includes("Сессия №6 · 06.07.2026"),
    true,
    "стрелка переключает подсказку на следующую сессию",
  );
  const dayCanvas = page.locator("[data-growth-day-chart]");
  await dayCanvas.scrollIntoViewIfNeeded();
  const dayBounds = await dayCanvas.boundingBox();
  if (!dayBounds) throw new Error("Не удалось определить положение графика дней");
  const dayPlotLeft = dayBounds.width < 520 ? 76 : 88;
  const dayPlotWidth = dayBounds.width - dayPlotLeft - 18;
  await page.mouse.move(
    dayBounds.x + dayPlotLeft + 4 * dayPlotWidth / 30,
    dayBounds.y + 50,
  );
  const dayTooltip = page.locator(".log-growth-tooltip").last();
  equal(await dayTooltip.isVisible(), true, "подсказка красной точки дня показана");
  const dayTooltipText = await dayTooltip.innerText();
  equal(dayTooltipText.includes("День · 05.07.2026"), true, "подсказка дня содержит полную дату");
  equal(dayTooltipText.includes("Исчерпания общего лимита\n1"), true, "дневная красная точка объяснена");

  await selectPreset(page, "current-month", "01.07.2026", "31.07.2026");
  await assertTotals(page, {
    sessions: "31",
    duration: "31 мин",
    generated: "39,5 МиБ",
    retained: "1 МиБ",
    fill: "100% лимита",
    reached: "6 сессий достигли лимита",
    "limit-reached": "21",
    rotations: "42",
    evicted: "21 МиБ",
  }, "текущий календарный месяц");
  equal(await page.locator("[data-growth-period-insight]").isVisible(), true, "оценка периода показана");
  equal(await page.locator("[data-growth-insight-title]").textContent(), "Есть отдельные остановки по лимиту", "оценка остановок по лимиту");
  equal(
    (await page.locator("[data-growth-insight-details]").textContent()).includes("Самый объёмный день — 30.07.2026: 7 МиБ"),
    true,
    "оценка называет самый объёмный день",
  );

  await selectPreset(page, "week", "25.07.2026", "31.07.2026");
  await assertTotals(page, {
    sessions: "7",
    duration: "7 мин",
    generated: "15,5 МиБ",
    reached: "2 сессий достигли лимита",
    "limit-reached": "11",
    rotations: "22",
    evicted: "11 МиБ",
  }, "последние семь дней");

  await selectPreset(page, "month", "02.07.2026", "31.07.2026");
  await assertTotals(page, {
    sessions: "30",
    duration: "30 мин",
    generated: "39 МиБ",
    "limit-reached": "21",
  }, "последние тридцать дней");

  await setCustomRange(page, "10.07.2026", "15.07.2026");
  await assertTotals(page, {
    sessions: "6",
    duration: "6 мин",
    generated: "9 МиБ",
    reached: "2 сессий достигли лимита",
    "limit-reached": "5",
    rotations: "10",
    evicted: "5 МиБ",
  }, "произвольный включительный промежуток");

  await setCustomRange(page, "01.07.2026", "04.07.2026");
  equal(await page.locator("[data-growth-insight-title]").textContent(), "Общий лимит не исчерпывался", "спокойная оценка периода");
  equal(
    (await page.locator("[data-growth-insight-summary]").textContent()).includes("50% общего бюджета"),
    true,
    "оценка показывает максимальное заполнение",
  );

  await selectPreset(page, "previous-month", "01.06.2026", "30.06.2026");
  equal(await page.locator("[data-growth-period-result]").isVisible(), false, "пустой месяц, итог скрыт");
  equal(await page.locator("[data-growth-period-insight]").isVisible(), false, "пустой месяц, оценка скрыта");
  equal(await page.locator("[data-growth-period-empty]").textContent(), "За выбранные дни данных нет.", "пустой месяц");

  await setCustomRange(page, "31.02.2026", "01.03.2026");
  equal(await page.locator("[data-growth-period-result]").isVisible(), false, "невозможная дата, итог скрыт");
  equal(
    await page.locator("[data-growth-period-empty]").textContent(),
    "Укажите правильные начальную и конечную даты.",
    "невозможная дата",
  );

  await setCustomRange(page, "31.07.2026", "01.07.2026");
  equal(await page.locator("[data-growth-period-result]").isVisible(), false, "обратный промежуток, итог скрыт");
  equal(
    await page.locator("[data-growth-period-empty]").textContent(),
    "Укажите правильные начальную и конечную даты.",
    "обратный промежуток",
  );

  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(150);
  await sessionCanvas.scrollIntoViewIfNeeded();
  const narrowCanvasBounds = await sessionCanvas.boundingBox();
  if (!narrowCanvasBounds) throw new Error("Не удалось определить положение узкого графика");
  const narrowPlotLeft = 76;
  const narrowPlotWidth = narrowCanvasBounds.width - narrowPlotLeft - 18;
  await page.mouse.move(
    narrowCanvasBounds.x + narrowPlotLeft + 9 * narrowPlotWidth / 30,
    narrowCanvasBounds.y + 24,
  );
  const narrowTooltipBounds = await sessionTooltip.boundingBox();
  const chartBounds = await page.locator(".log-growth-chart").first().boundingBox();
  if (!narrowTooltipBounds || !chartBounds) throw new Error("Не удалось определить границы подсказки");
  equal(narrowTooltipBounds.x >= chartBounds.x, true, "узкая подсказка не выходит слева за график");
  equal(
    narrowTooltipBounds.x + narrowTooltipBounds.width <= chartBounds.x + chartBounds.width + 1,
    true,
    "узкая подсказка не выходит справа за график",
  );
  equal(
    await page.evaluate(() => document.documentElement.scrollWidth === document.documentElement.clientWidth),
    true,
    "отчёт не получил горизонтальную прокрутку",
  );

  const paginationPage = await browser.newPage({ locale: "ru-RU" });
  await paginationPage.goto(pathToFileURL(paginationReportPath).href);
  await paginationPage.locator("#log-growth summary").click();
  const sessionRows = paginationPage.locator("[data-growth-session-rows] tr:not(.deferred-table-loader)");
  const sessionLoader = paginationPage.locator("[data-growth-session-rows] .deferred-table-loader");
  const dayRows = paginationPage.locator("[data-growth-day-rows] tr:not(.deferred-table-loader)");
  const dayLoader = paginationPage.locator("[data-growth-day-rows] .deferred-table-loader");
  await sessionRows.first().waitFor();
  equal(await sessionRows.count(), 50, "таблица сессий изначально показывает 50 строк");
  equal(await dayRows.count(), 50, "таблица дней изначально показывает 50 строк");
  equal(await sessionLoader.locator("button").textContent(), "Показать ещё 50", "первая страница сессий");
  equal(await sessionLoader.locator("[data-deferred-remaining]").textContent(), "Осталось строк: 70", "остаток сессий");
  equal(await dayLoader.locator("button").textContent(), "Показать ещё 50", "первая страница дней");

  await sessionLoader.locator("button").click();
  equal(await sessionRows.count(), 100, "вторая страница добавляет ещё 50 сессий");
  equal(await sessionLoader.locator("button").textContent(), "Показать ещё 20", "последняя страница сессий");
  equal(await sessionLoader.locator("[data-deferred-remaining]").textContent(), "Осталось строк: 20", "последний остаток сессий");
  await sessionLoader.locator("button").click();
  equal(await sessionRows.count(), 120, "таблица сессий раскрывается полностью");
  equal(await sessionLoader.count(), 0, "кнопка сессий исчезает после полной загрузки");

  await dayLoader.locator("button").click();
  equal(await dayRows.count(), 100, "вторая страница добавляет ещё 50 дней");
  equal(await dayLoader.locator("button").textContent(), "Показать ещё 20", "последняя страница дней");
  await paginationPage.close();

  process.stdout.write("Проверка расчётов роста журналов в HTML-отчёте пройдена.\n");
} finally {
  if (browser) await browser.close();
  rmSync(temporaryDirectory, { recursive: true, force: true });
}
