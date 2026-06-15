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
      const issues = await page.evaluate(() => {
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
          .filter((row) => row.height > 320 && row.text.length > 0);
        const clippedTooltips = Array.from(document.querySelectorAll("[data-tip]"))
          .filter((node) => {
            const rect = node.getBoundingClientRect();
            return rect.width === 0 || rect.height === 0;
          })
          .length;
        return { pageOverflow, bareTables, tallRows, clippedTooltips };
      });

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
