import { spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";
import { validateConcurrencyPerformance } from "./concurrency-performance-budget.mjs";

const cliRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const fixtureDir = mkdtempSync(join(tmpdir(), "jankhunter-causal-performance-"));
const fixturePath = join(fixtureDir, "concurrency.html");
const assert = (condition, message) => { if (!condition) throw new Error(message); };
const percentile = (values, p) => {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * p) - 1)];
};

let browser;
try {
  const generated = spawnSync("go", ["test", "./internal/report", "-run", "^TestWriteCausalPerformanceFixture$", "-count=1", "-v"], {
    cwd: cliRoot, encoding: "utf8", env: {...process.env, JH_CAUSAL_BENCH_OUT: fixturePath},
  });
  if (generated.status !== 0) throw new Error(`Go fixture failed\n${generated.stdout}\n${generated.stderr}`);
  const fixtureLog = generated.stdout.match(/causal fixture: .*/)?.[0] || "no Go timing";
  browser = await chromium.launch();
  const page = await browser.newPage({viewport: {width: 1440, height: 1000}});
  const errors = [];
  page.on("pageerror", error => errors.push(String(error)));
  const start = performance.now();
  await page.goto(pathToFileURL(fixturePath).href, {waitUntil: "load"});
  await page.locator("#concurrency-findings .concurrency-finding").first().waitFor();
  const firstFindingMs = performance.now() - start;
  const initialDOM = await page.locator("*").count();
  assert(firstFindingMs <= 5_000, `first actionable finding took ${firstFindingMs.toFixed(1)}ms`);

  const issueAnchor = await page.locator("#concurrency-findings .concurrency-finding a[href^='#causal-']").first().getAttribute("href");
  const openStart = performance.now();
  await page.locator("#concurrency-findings .concurrency-finding a[href^='#causal-']").first().click();
  await page.locator(`[data-causal-list] ${issueAnchor}`).waitFor();
  const issueOpenMs = performance.now() - openStart;
  const openDOM = await page.locator("*").count();
  const cardsAfterOpen = await page.locator("[data-causal-list] > .concurrency-entity").count();
  assert(cardsAfterOpen <= 20, `unbounded causal cards: ${cardsAfterOpen}`);
  assert(await page.locator("[data-causal-status]").textContent().then(text => text.includes("10500")), "full index count absent");
  await page.locator(`[data-causal-list] ${issueAnchor} a[href='#causal-benchmark-1-500']`).first().click();
  await page.locator("[data-causal-list] #causal-benchmark-1-500").waitFor();

  const timings = await page.evaluate(async () => {
    const scope = document.querySelector("[data-causal-scope]");
    const search = document.querySelector("[data-causal-search]");
    const next = document.querySelector("[data-causal-next]");
    const status = document.querySelector("[data-causal-status]");
    const issueLink = document.querySelector("#concurrency-findings .concurrency-finding a[href^='#causal-']");
    const samples = {scope: [], page: [], issue: [], searchVisible: []};
    for (let i = 0; i < 30; i++) {
      scope.value = i % 2 ? "all" : "app";
      let start = performance.now();
      scope.dispatchEvent(new Event("change", {bubbles: true}));
      samples.scope.push(performance.now() - start);
      start = performance.now();
      next.click();
      samples.page.push(performance.now() - start);
      start = performance.now();
      issueLink.click();
      samples.issue.push(performance.now() - start);
    }
    for (let i = 0; i < 20; i++) {
      search.value = i % 2 ? "10000" : "app.benchmark.Work";
      let observer;
      const rendered = new Promise((resolve, reject) => {
        const timeout = setTimeout(() => { observer.disconnect(); reject(new Error("causal search did not render")); }, 1_000);
        observer = new MutationObserver(() => { clearTimeout(timeout); observer.disconnect(); resolve(); });
        observer.observe(status, {childList: true, characterData: true, subtree: true});
      });
      const start = performance.now();
      search.dispatchEvent(new Event("input", {bubbles: true}));
      await rendered;
      samples.searchVisible.push(performance.now() - start);
    }
    return samples;
  });
  const cardsAfterSearch = await page.locator("[data-causal-list] > .concurrency-entity").count();
  assert(cardsAfterSearch <= 20, `search created ${cardsAfterSearch} cards`);
  assert(errors.length === 0, `browser JS errors: ${errors.join("; ")}`);
  const output = {
    fixture: fixtureLog,
    htmlMiB: +(statSync(fixturePath).size / 1048576).toFixed(2),
    firstFindingMs: +firstFindingMs.toFixed(1), issueOpenMs: +issueOpenMs.toFixed(1),
    initialDOM, openDOM, cardsAfterOpen, cardsAfterSearch,
    scopeP50Ms: +percentile(timings.scope, .5).toFixed(1), scopeP95Ms: +percentile(timings.scope, .95).toFixed(1),
    pageP50Ms: +percentile(timings.page, .5).toFixed(1), pageP95Ms: +percentile(timings.page, .95).toFixed(1),
    issueP50Ms: +percentile(timings.issue, .5).toFixed(1), issueP95Ms: +percentile(timings.issue, .95).toFixed(1),
    searchVisibleP50Ms: +percentile(timings.searchVisible, .5).toFixed(1),
    searchVisibleP95Ms: +percentile(timings.searchVisible, .95).toFixed(1),
    errors,
  };
  console.log(JSON.stringify(output, null, 2));
  validateConcurrencyPerformance(output);
} finally {
  if (browser) await browser.close();
  rmSync(fixtureDir, {recursive: true, force: true});
}
