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

const samplePath = resolve(outDir, "sample.jhlog");
const inspectPath = resolve(outDir, "inspect.html");
const comparePath = resolve(outDir, "compare.html");

run(["sample", "--out", samplePath]);
run(["inspect", samplePath, "--out", inspectPath]);
run(["compare", "--baseline", samplePath, "--candidate", samplePath, "--out", comparePath]);

const reportPaths = [
  inspectPath,
  inspectPath.replace(/\.html$/, "-math.html"),
  inspectPath.replace(/\.html$/, "-influence.html"),
  comparePath,
  comparePath.replace(/\.html$/, "-math.html"),
  comparePath.replace(/\.html$/, "-influence.html"),
].filter(existsSync);

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
    for (const reportPath of reportPaths) {
      const reportName = reportPath
        .slice(outDir.length + 1)
        .replace(/[^a-z0-9-]+/gi, "-")
        .replace(/-html$/, "");
      await page.goto(pathToFileURL(reportPath).href, { waitUntil: "load" });
      await page.evaluate(() => document.fonts?.ready);
      await page.waitForTimeout(120);
      const issues = await collectLayoutIssues(page);
      const longCellToggle = await checkLongCellToggle(page);
      const zeroToggle = await checkZeroToggle(page);
      const tooltipIssue = await checkTooltipPlacement(page);

      if (issues.pageOverflow > 6) {
        failures.push(`${viewport.name}/${reportName}: страница шире viewport на ${issues.pageOverflow}px`);
      }
      if (issues.bareTables.length > 0) {
        failures.push(`${viewport.name}/${reportName}: таблицы без горизонтального скролла: ${issues.bareTables.length}`);
      }
      if (issues.tallRows.length > 0) {
        failures.push(`${viewport.name}/${reportName}: слишком высокие строки таблиц: ${JSON.stringify(issues.tallRows.slice(0, 3))}`);
      }
      if (issues.clippedTooltips > 0) {
        failures.push(`${viewport.name}/${reportName}: скрытые элементы с подсказками: ${issues.clippedTooltips}`);
      }
      if (issues.scrollWrappers > 0) {
        failures.push(`${viewport.name}/${reportName}: table-scroll не дает горизонтальный скролл для ${issues.scrollWrappers} таблиц`);
      }
      if (issues.clippedCells > 0 && !longCellToggle.available) {
        failures.push(`${viewport.name}/${reportName}: есть обрезанные длинные ячейки без кнопки раскрытия`);
      }
      if (issues.nakedOverflowCells.length > 0) {
        failures.push(`${viewport.name}/${reportName}: ячейки вне table-scroll выходят за границы: ${JSON.stringify(issues.nakedOverflowCells.slice(0, 3))}`);
      }
      if (longCellToggle.available && (!longCellToggle.expanded || !longCellToggle.collapsed)) {
        failures.push(`${viewport.name}/${reportName}: кнопка раскрытия длинной ячейки не переключает состояние`);
      }
      if (zeroToggle.available && zeroToggle.zeroRows > 0) {
        if (zeroToggle.hiddenBefore === 0 || zeroToggle.visibleAfter === 0 || zeroToggle.hiddenAfter === 0) {
          failures.push(`${viewport.name}/${reportName}: переключатель нулевых бакетов не меняет строки (${JSON.stringify(zeroToggle)})`);
        }
      }
      if (tooltipIssue) {
        failures.push(`${viewport.name}/${reportName}: ${tooltipIssue}`);
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
