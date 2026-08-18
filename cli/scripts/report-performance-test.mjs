import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cliRoot = resolve(scriptDir, "..");
const fixtureDir = mkdtempSync(join(tmpdir(), "jankhunter-report-performance-"));
const fixturePath = join(fixtureDir, "large-report.html");
const deferredFixturePath = join(fixtureDir, "deferred-table-report.html");
const deferredRegistryPath = join(fixtureDir, "deferred-registry-report.html");

const assert = (condition, message) => {
  if (!condition) throw new Error(message);
};

const buildFixture = () => {
  const result = spawnSync(
    "go",
    ["test", "./internal/report", "-run", "^TestWriteLargeBundlePerformanceFixture$", "-count=1"],
    {
      cwd: cliRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        JH_LARGE_BUNDLE_OUT: fixturePath,
        JH_DEFERRED_REPORT_OUT: deferredFixturePath,
        JH_DEFERRED_REGISTRY_OUT: deferredRegistryPath,
      },
    },
  );
  if (result.status !== 0) {
    throw new Error(`Не удалось создать большой HTML fixture\n${result.stdout}\n${result.stderr}`);
  }
};

const pageState = async (frame) => frame.evaluate(() => ({
  domNodes: document.querySelectorAll("*").length,
  enhancedCells: document.querySelectorAll("[data-cell-enhanced=true]").length,
  observedRows: document.querySelectorAll("[data-cell-row-observed=true]").length,
  rows: document.querySelectorAll("tbody tr").length,
  tooltipTargets: document.querySelectorAll("[data-tip]").length,
}));

let browser;
try {
  buildFixture();
  browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  const started = Date.now();
  await page.goto(pathToFileURL(fixturePath).href, { waitUntil: "load" });
  const frameElement = await page.waitForSelector('iframe.report-frame.active[data-page="page-0"]');
  const frame = await frameElement.contentFrame();
  assert(frame, "Первый раздел большого отчета не загрузился");
  await frame.waitForLoadState("load");
  await page.waitForTimeout(500);
  const interactiveMs = Date.now() - started;

  const manifest = await page.evaluate(() => ({
    chars: document.getElementById("jankhunter-report-pages")?.textContent.length || 0,
    frames: document.querySelectorAll("iframe").length,
    payloads: document.querySelectorAll("[data-jankhunter-report-payload]").length,
  }));
  const initial = await pageState(frame);
  await page.waitForTimeout(2_000);
  const settled = await pageState(frame);

  assert(manifest.chars <= 2_048, `manifest содержит ${manifest.chars} символов, ожидалось <= 2048`);
  assert(manifest.frames === 1, `eager iframe count = ${manifest.frames}, ожидался 1`);
  assert(manifest.payloads === 5, `неоткрытых payload = ${manifest.payloads}, ожидалось 5`);
  assert(initial.rows === 12_000, `fixture содержит ${initial.rows} строк, ожидалось 12000`);
  assert(initial.enhancedCells <= 64, `eager enhanced cells = ${initial.enhancedCells}, ожидалось <= 64`);
  assert(settled.domNodes === initial.domNodes, `DOM вырос в idle: ${initial.domNodes} -> ${settled.domNodes}`);
  assert(settled.enhancedCells === initial.enhancedCells, `idle enhancement продолжился: ${initial.enhancedCells} -> ${settled.enhancedCells}`);
  assert(settled.tooltipTargets === initial.tooltipTargets, `idle tooltip materialization продолжилась: ${initial.tooltipTargets} -> ${settled.tooltipTargets}`);

  await page.getByRole("tab", { name: "Большой раздел 2" }).click();
  await page.waitForSelector('iframe.report-frame.active[data-page="page-1"]');
  const switched = await page.evaluate(() => ({
    frames: document.querySelectorAll("iframe").length,
    payloads: document.querySelectorAll("[data-jankhunter-report-payload]").length,
  }));
  assert(switched.frames === 2, `после открытия второго раздела iframe count = ${switched.frames}, ожидалось 2`);
  assert(switched.payloads === 4, `после открытия второго раздела payload = ${switched.payloads}, ожидалось 4`);

  await page.getByRole("tab", { name: "Большой раздел 1" }).click();
  await frame.locator("tbody tr").last().locator("td").first().locator("code").click();
  await page.waitForTimeout(300);
  const scrolled = await pageState(frame);
  assert(scrolled.enhancedCells > initial.enhancedCells, "строки около новой позиции viewport не были обработаны");
  assert(scrolled.enhancedCells < 256, `после дальнего scroll обработано ${scrolled.enhancedCells} ячеек, ожидалось < 256`);
  assert(scrolled.tooltipTargets > initial.tooltipTargets, "tooltip наведенной ячейки не был материализован лениво");

  const deferredPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await deferredPage.goto(pathToFileURL(deferredFixturePath).href, { waitUntil: "load" });
  const counterBody = deferredPage.locator('tbody:has(> tr[data-deferred-total="12000"])');
  const deferredInitial = {
    rows: await counterBody.locator("tr:not([data-deferred-loader])").count(),
    payloads: await counterBody.locator("script[data-table-chunk]").count(),
    domNodes: await deferredPage.locator("*").count(),
  };
  assert(deferredInitial.rows === 50, `первоначально создано ${deferredInitial.rows} строк, ожидалось 50`);
  assert(deferredInitial.payloads === 239, `порций таблицы = ${deferredInitial.payloads}, ожидалось 239`);
  await deferredPage.getByText("Детали метрик", { exact: true }).click();
  await counterBody.getByRole("button", { name: "Показать ещё 50" }).click();
  await deferredPage.waitForFunction(() => {
    const loader = document.querySelector('tr[data-deferred-total="12000"]');
    return loader?.parentElement?.querySelectorAll('tr:not([data-deferred-loader])').length === 100;
  });
  const deferredLoaded = {
    rows: await counterBody.locator("tr:not([data-deferred-loader])").count(),
    payloads: await counterBody.locator("script[data-table-chunk]").count(),
    remaining: await counterBody.locator("[data-deferred-remaining]").textContent(),
  };
  assert(deferredLoaded.rows === 100, `после клика создано ${deferredLoaded.rows} строк, ожидалось 100`);
  assert(deferredLoaded.payloads === 238, `после клика осталось ${deferredLoaded.payloads} порций, ожидалось 238`);
  assert(deferredLoaded.remaining?.includes("11900"), `неверный счетчик оставшихся строк: ${deferredLoaded.remaining}`);
  await deferredPage.close();

  const registryPage = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await registryPage.goto(pathToFileURL(deferredRegistryPath).href, { waitUntil: "load" });
  const registry = registryPage.locator("[data-code-registry]").first();
  const registryInitial = {
    rows: await registry.locator("[data-code-problem-row]").count(),
    archives: await registry.locator("script[data-code-problem-evidence-archive]").count(),
    remaining: await registry.locator("[data-deferred-remaining]").textContent(),
  };
  assert(registryInitial.rows === 50, `архивный реестр создал ${registryInitial.rows} строк, ожидалось 50`);
  assert(registryInitial.archives === 1, `архивов evidence = ${registryInitial.archives}, ожидался 1`);
  assert(registryInitial.remaining?.includes("250"), `неверный начальный остаток архивного реестра: ${registryInitial.remaining}`);
  await registry.getByRole("button", { name: "Показать ещё 50" }).click();
  await registryPage.waitForFunction(() => document.querySelectorAll("[data-code-problem-row]").length === 100);
  const registryLoaded = {
    rows: await registry.locator("[data-code-problem-row]").count(),
    remaining: await registry.locator("[data-deferred-remaining]").textContent(),
  };
  assert(registryLoaded.rows === 100, `архивный реестр загрузил ${registryLoaded.rows} строк, ожидалось 100`);
  assert(registryLoaded.remaining?.includes("200"), `неверный остаток архивного реестра: ${registryLoaded.remaining}`);
  await registry.locator("[data-code-registry-search]").fill("DeferredProblem299");
  await registryPage.waitForFunction(() => document.querySelector("[data-code-registry-count]")?.textContent === "1 из 300");
  const registryFiltered = {
    rows: await registry.locator("[data-code-problem-row]").count(),
    visibleRows: await registry.locator("[data-code-problem-row]:not([hidden])").count(),
    archives: await registry.locator("script[data-code-problem-evidence-archive]").count(),
    counter: await registry.locator("[data-code-registry-count]").textContent(),
  };
  assert(registryFiltered.rows === 1, `фильтр материализовал ${registryFiltered.rows} строк, ожидалась только совпавшая строка`);
  assert(registryFiltered.visibleRows === 1, `фильтр оставил ${registryFiltered.visibleRows} строк, ожидалась 1`);
  assert(registryFiltered.archives === 0, `после декодирования остался evidence archive: ${registryFiltered.archives}`);
  const filteredDetails = registry.locator(".code-problem-details").first();
  await filteredDetails.locator("summary").click();
  await registryPage.waitForFunction(() => document.querySelector(".code-problem-details")?.dataset.evidenceLoaded === "true");
  const expandedEvidence = await filteredDetails.textContent();
  assert(expandedEvidence.includes("Deferred signal 299"), "хвостовой сигнал не раскрылся из evidence archive");
  assert(expandedEvidence.includes("deferred.flow.299"), "хвостовой сценарий не раскрылся из evidence archive");
  await registryPage.close();

  process.stdout.write(`${JSON.stringify({
    htmlBytes: statSync(fixturePath).size,
    interactiveMs,
    manifestChars: manifest.chars,
    initial,
    settled,
    scrolled,
    deferredInitial,
    deferredLoaded,
    registryInitial,
    registryLoaded,
    registryFiltered,
  })}\n`);
} finally {
  await browser?.close();
  rmSync(fixtureDir, { recursive: true, force: true });
}
