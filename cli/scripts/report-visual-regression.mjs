import { spawnSync } from "node:child_process";
import { mkdirSync, existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const scriptDir = dirname(fileURLToPath(import.meta.url));
const cliRoot = resolve(scriptDir, "..");
const repoRoot = resolve(cliRoot, "..");
const outDir = resolve(process.env.JH_VISUAL_OUT || resolve(repoRoot, "tmp", "report-visual-regression"));

mkdirSync(outDir, { recursive: true });

const run = (args) => {
  const result = spawnSync("go", ["run", "./cmd/jankhunter", ...args], {
    cwd: cliRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  });
  if (result.status !== 0) {
    throw new Error(`go run ./cmd/jankhunter ${args.join(" ")}\n${result.stdout}\n${result.stderr}`);
  }
};

const buildReportSet = (name, logCount, presentation = false) => {
  const setDir = resolve(outDir, name);
  mkdirSync(setDir, { recursive: true });
  const logs = [];
  for (let index = 0; index < logCount; index += 1) {
    const logPath = resolve(setDir, `sample-${index + 1}.jhlog`);
    run(["sample", "--out", logPath]);
    logs.push(logPath);
  }

  const inspectPath = resolve(setDir, "inspect.html");
  const comparePath = resolve(setDir, "compare.html");
  const presentationFlag = presentation ? ["--presentation"] : [];
  run(["inspect", ...logs, ...presentationFlag, "--out", inspectPath]);
  run([
    "compare",
    "--baseline", logs.join(","),
    "--candidate", logs.join(","),
    ...presentationFlag,
    "--out", comparePath,
  ]);

  const reports = [
    { set: name, type: "inspect", path: inspectPath },
    { set: name, type: "inspect-math", path: inspectPath.replace(/\.html$/, "-math.html") },
    { set: name, type: "inspect-influence", path: inspectPath.replace(/\.html$/, "-influence.html") },
    { set: name, type: "compare", path: comparePath },
    { set: name, type: "compare-math", path: comparePath.replace(/\.html$/, "-math.html") },
    { set: name, type: "compare-influence", path: comparePath.replace(/\.html$/, "-influence.html") },
  ].filter((report) => existsSync(report.path));

  for (const required of ["inspect", "inspect-math", "inspect-influence", "compare", "compare-math", "compare-influence"]) {
    if (!reports.some((report) => report.type === required)) {
      throw new Error(`В snapshot-наборе ${name} не создан отчет ${required}`);
    }
  }
  return reports;
};

const reportPaths = [
  ...buildReportSet("short", 1, false),
  ...buildReportSet("long-presentation", 8, true),
];

const browser = await chromium.launch();
const viewports = [
  { name: "desktop", width: 1440, height: 1000 },
  { name: "mobile", width: 390, height: 844 },
];
const failures = [];

const collectLayoutIssues = async (page) => page.evaluate(() => {
  const root = document.documentElement;
  const pageOverflow = Math.max(0, root.scrollWidth - root.clientWidth);
  const bareTables = Array.from(document.querySelectorAll("table"))
    .filter((table) => !table.closest(".table-scroll"))
    .map((table) => table.textContent.trim().slice(0, 120));
  const tallRows = Array.from(document.querySelectorAll("tr"))
    .map((row) => ({
      height: row.getBoundingClientRect().height,
      text: row.textContent.trim().replace(/\s+/g, " ").slice(0, 160),
    }))
    .filter((row) => row.height > 340 && row.text.length > 0);
  const clippedTooltips = Array.from(document.querySelectorAll("[data-tip]"))
    .filter((node) => {
      const rect = node.getBoundingClientRect();
      return rect.width === 0 || rect.height === 0;
    })
    .length;
  const scrollWrappers = Array.from(document.querySelectorAll(".table-scroll"))
    .filter((wrapper) => {
      const table = wrapper.querySelector("table");
      return table && table.scrollWidth > wrapper.clientWidth + 4 && wrapper.scrollWidth <= wrapper.clientWidth + 4;
    })
    .length;
  const clippedCells = Array.from(document.querySelectorAll(".table-cell-clip"))
    .filter((cell) => cell.scrollHeight > cell.clientHeight + 4)
    .length;
  const nakedOverflowCells = Array.from(document.querySelectorAll("td, th"))
    .filter((cell) => !cell.closest(".table-scroll") && cell.scrollWidth > cell.clientWidth + 4)
    .map((cell) => cell.textContent.trim().replace(/\s+/g, " ").slice(0, 120));
  return { pageOverflow, bareTables, tallRows, clippedTooltips, scrollWrappers, clippedCells, nakedOverflowCells };
});

const checkLongCellToggle = async (page) => page.evaluate(() => {
  const toggle = document.querySelector(".cell-toggle");
  const cell = document.querySelector(".table-cell-clip");
  if (!toggle || !cell) {
    return { available: false, expanded: false, collapsed: false };
  }
  toggle.click();
  const expanded = cell.classList.contains("is-expanded");
  toggle.click();
  const collapsed = !cell.classList.contains("is-expanded");
  return { available: true, expanded, collapsed };
});

const checkZeroToggle = async (page) => page.evaluate(() => {
  const toggle = document.querySelector("[data-zero-toggle]");
  if (!toggle) {
    return { available: false, zeroRows: 0, hiddenBefore: 0, visibleAfter: 0, hiddenAfter: 0 };
  }
  const scope = toggle.closest("[data-zero-scope]") || document.body;
  const rows = Array.from(scope.querySelectorAll(".bucket-zero"));
  const visibleCount = () => rows.filter((row) => getComputedStyle(row).display !== "none").length;
  toggle.checked = false;
  toggle.dispatchEvent(new Event("change", { bubbles: true }));
  const hiddenBefore = rows.length - visibleCount();
  toggle.checked = true;
  toggle.dispatchEvent(new Event("change", { bubbles: true }));
  const visibleAfter = visibleCount();
  toggle.checked = false;
  toggle.dispatchEvent(new Event("change", { bubbles: true }));
  const hiddenAfter = rows.length - visibleCount();
  return { available: true, zeroRows: rows.length, hiddenBefore, visibleAfter, hiddenAfter };
});

const checkTooltipPlacement = async (page) => {
  const boxes = await page.$$eval("[data-tip]", (nodes) => nodes
    .map((node) => {
      const rect = node.getBoundingClientRect();
      return {
        x: rect.left + rect.width / 2,
        y: rect.top + rect.height / 2,
        visible: rect.width > 0 && rect.height > 0 && rect.bottom > 0 && rect.right > 0 && rect.top < window.innerHeight && rect.left < window.innerWidth,
      };
    })
    .filter((box) => box.visible));
  let checked = 0;
  for (const box of boxes.slice(0, 12)) {
    await page.mouse.move(box.x, box.y);
    await page.waitForTimeout(40);
    const issue = await page.evaluate(() => {
      const tip = document.querySelector(".jh-tooltip.is-visible");
      if (!tip) return "подсказка не появилась";
      const rect = tip.getBoundingClientRect();
      const margin = 4;
      if (rect.left < margin || rect.right > window.innerWidth - margin) return "подсказка вышла за горизонтальные границы";
      if (rect.top < margin || rect.bottom > window.innerHeight - margin) return "подсказка вышла за вертикальные границы";
      return "";
    });
    if (issue) {
      return issue;
    }
    checked += 1;
  }
  return checked > 0 ? "" : "";
};

try {
  for (const viewport of viewports) {
    const page = await browser.newPage({ viewport, deviceScaleFactor: 1 });
    for (const report of reportPaths) {
      const reportName = `${report.set}-${report.type}`;
      await page.goto(pathToFileURL(report.path).href, { waitUntil: "load" });
      await page.evaluate(() => document.fonts?.ready);
      await page.waitForTimeout(120);
      const issues = await collectLayoutIssues(page);
      const longCellToggle = await checkLongCellToggle(page);
      const zeroToggle = await checkZeroToggle(page);
      const tooltipIssue = await checkTooltipPlacement(page);

      const displayName = report.path
        .slice(outDir.length + 1)
        .replace(/[^a-z0-9-]+/gi, "-")
        .replace(/-html$/, "");

      if (issues.pageOverflow > 6) {
        failures.push(`${viewport.name}/${displayName}: страница шире viewport на ${issues.pageOverflow}px`);
      }
      if (issues.bareTables.length > 0) {
        failures.push(`${viewport.name}/${displayName}: таблицы без горизонтального скролла: ${issues.bareTables.length}`);
      }
      if (issues.tallRows.length > 0) {
        failures.push(`${viewport.name}/${displayName}: слишком высокие строки таблиц: ${JSON.stringify(issues.tallRows.slice(0, 3))}`);
      }
      if (issues.clippedTooltips > 0) {
        failures.push(`${viewport.name}/${displayName}: скрытые элементы с подсказками: ${issues.clippedTooltips}`);
      }
      if (issues.scrollWrappers > 0) {
        failures.push(`${viewport.name}/${displayName}: table-scroll не дает горизонтальный скролл для ${issues.scrollWrappers} таблиц`);
      }
      if (issues.clippedCells > 0 && !longCellToggle.available) {
        failures.push(`${viewport.name}/${displayName}: есть обрезанные длинные ячейки без кнопки раскрытия`);
      }
      if (issues.nakedOverflowCells.length > 0) {
        failures.push(`${viewport.name}/${displayName}: ячейки вне table-scroll выходят за границы: ${JSON.stringify(issues.nakedOverflowCells.slice(0, 3))}`);
      }
      if (longCellToggle.available && (!longCellToggle.expanded || !longCellToggle.collapsed)) {
        failures.push(`${viewport.name}/${displayName}: кнопка раскрытия длинной ячейки не переключает состояние`);
      }
      if (zeroToggle.available && zeroToggle.zeroRows > 0) {
        if (zeroToggle.hiddenBefore === 0 || zeroToggle.visibleAfter === 0 || zeroToggle.hiddenAfter === 0) {
          failures.push(`${viewport.name}/${displayName}: переключатель нулевых бакетов не меняет строки (${JSON.stringify(zeroToggle)})`);
        }
      }
      if (tooltipIssue) {
        failures.push(`${viewport.name}/${displayName}: ${tooltipIssue}`);
      }

      await page.screenshot({
        path: resolve(outDir, `${reportName}-${viewport.name}.png`),
        fullPage: true,
      });
    }
    await page.close();
  }
} finally {
  await browser.close();
}

if (failures.length > 0) {
  console.error("Report visual regression failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  console.error(`Artifacts: ${outDir}`);
  process.exit(1);
}

console.log(`Report visual regression passed. Artifacts: ${outDir}`);
