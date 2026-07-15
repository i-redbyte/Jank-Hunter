import { spawnSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
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

const buildReportSet = (name, presentation = false) => {
  const setDir = resolve(outDir, name);
  mkdirSync(setDir, { recursive: true });
  const logPath = resolve(setDir, "sample.jhlog");
  const candidatePath = resolve(setDir, "candidate.jhlog");
  run(["sample", "--out", logPath]);
  run(["sample", "--out", candidatePath]);
  const logs = [logPath];
  const candidateLogs = [candidatePath];

  const inspectPath = resolve(setDir, "inspect.html");
  const comparePath = resolve(setDir, "compare.html");
  const presentationFlag = presentation ? ["--presentation"] : [];
  const diagnosticsPath = resolve(setDir, "instrumentation-diagnostics.jsonl");
  writeFileSync(diagnosticsPath, [
    JSON.stringify({
      format: 1,
      class: "com.app.feed.FeedRepository",
      methods: 12,
      ignoredMethods: 1,
      annotatedMethods: 3,
      skippedMethods: [{ reason: "constructor", count: 1 }],
      hooks: [
        { intent: "okhttp.install_event_listener_factory", signature: "okhttp3.builder.build.v3", bridge: "okhttp3.bridge.v3", count: 4 },
        { intent: "logspam.android.util.Log.d", signature: "logspam.android.util.Log.d", count: 9 },
      ],
      decisions: [{ kind: "unsupported", module: "okhttp", family: "okhttp", reason: "unsupported_signature", count: 2 }],
      annotations: [{ owner: "FeedOwner", screen: "Feed", flow: "feed.open", trace: "refresh", count: 3 }],
    }),
    JSON.stringify({
      format: 1,
      class: "com.app.checkout.CheckoutPresenter",
      methods: 9,
      ignoredMethods: 0,
      annotatedMethods: 2,
      skippedMethods: [],
      hooks: [
        { intent: "coroutine.wrap_block.function2_before_continuation", signature: "kotlinx.coroutines.suspend_builders.function2_continuation.v1", bridge: "kotlinx.coroutines.bridge.v1", count: 2 },
      ],
      annotations: [{ owner: "CheckoutPresenter", screen: "Checkout", flow: "checkout.pay", trace: "submit", count: 2 }],
    }),
  ].join("\n") + "\n");
  const diagnosticsArgs = ["--instrumentation-diagnostics", diagnosticsPath];
  const ownerMapArgs = [];
  if (presentation) {
    const ownerMapPath = resolve(setDir, "owner-map.json");
    writeFileSync(ownerMapPath, JSON.stringify({
      format: 4,
      kind: "metadata",
      symbolNamespace: "00112233445566778899aabbccddeeff",
      owners: {
        "stable:0x0000000000001001": "registration.ui.RegistrationActivity ru.mail.instantmessenger.flat.main.MainActivity __jh_dictionary_overflow__ click",
        "stable:0x0000000000001002": "lifecycle.destroyed.ru.mail.instantmessenger.flat.main.MainActivity",
        "stable:0x0000000000001003": "ru.mail.instantmessenger.flat.main.MainActivity.render.__jh_dictionary_overflow__.bind",
      },
    }, null, 2));
    ownerMapArgs.push("--owner-map", ownerMapPath);
  }
  run(["inspect", ...logs, ...ownerMapArgs, ...diagnosticsArgs, ...presentationFlag, "--out", inspectPath]);
  run([
    "compare",
    "--baseline", logs.join(","),
    "--candidate", candidateLogs.join(","),
    ...ownerMapArgs,
    ...diagnosticsArgs,
    ...presentationFlag,
    "--out", comparePath,
  ]);

  const reports = [
    { set: name, type: "inspect", path: inspectPath, page: "overview" },
    { set: name, type: "inspect-math", path: inspectPath, page: "math" },
    { set: name, type: "inspect-leaks", path: inspectPath, page: "leaks" },
    { set: name, type: "inspect-influence", path: inspectPath, page: "influence" },
    { set: name, type: "inspect-diagnostics", path: inspectPath, page: "diagnostics" },
    { set: name, type: "compare", path: comparePath, page: "overview" },
    { set: name, type: "compare-math", path: comparePath, page: "math" },
    { set: name, type: "compare-leaks", path: comparePath, page: "leaks" },
    { set: name, type: "compare-influence", path: comparePath, page: "influence" },
    { set: name, type: "compare-diagnostics", path: comparePath, page: "diagnostics" },
  ];

  for (const required of ["inspect", "inspect-math", "inspect-leaks", "inspect-influence", "inspect-diagnostics", "compare", "compare-math", "compare-leaks", "compare-influence", "compare-diagnostics"]) {
    if (!reports.some((report) => report.type === required)) {
      throw new Error(`В snapshot-наборе ${name} не создан отчет ${required}`);
    }
  }
  return reports;
};

const reportPaths = [
  ...buildReportSet("short"),
  ...buildReportSet("long-presentation", true),
];

const browser = await chromium.launch();
const viewports = [
  { name: "desktop", width: 1440, height: 1000 },
  { name: "mobile", width: 390, height: 844 },
];
const visualStabilityCSS = `
  *, *::before, *::after {
    animation: none !important;
    scroll-behavior: auto !important;
    transition: none !important;
  }
`;
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
  const escapedProblemCells = Array.from(document.querySelectorAll(".code-problem-table td, .leak-table td"))
    .filter((cell) => {
      const cellRect = cell.getBoundingClientRect();
      if (cellRect.width <= 0 || cellRect.height <= 0) return false;
      return Array.from(cell.querySelectorAll("code, .problem-chip, .problem-score, .problem-signal, .leak-dominator span, .table-cell-clip"))
        .some((node) => {
          const rect = node.getBoundingClientRect();
          return rect.width > 0 && (rect.left < cellRect.left - 4 || rect.right > cellRect.right + 4);
        });
    })
    .map((cell) => cell.textContent.trim().replace(/\s+/g, " ").slice(0, 160));
  const graphEdges = Array.from(document.querySelectorAll(".leak-graph-edge, .influence-edge"));
  const missingArrowMarkers = graphEdges.filter((edge) => {
    const marker = edge.getAttribute("marker-end") || "";
    const match = marker.match(/^url\(#([^)]+)\)$/);
    return !match || !document.getElementById(match[1]);
  }).length;
  const leakNodes = Array.from(document.querySelectorAll(".leak-graph-node"));
  const leakLabelOverlaps = Array.from(document.querySelectorAll(".leak-graph-edge-label-bg"))
    .filter((label) => {
      const a = label.getBoundingClientRect();
      return leakNodes.some((node) => {
        const b = node.getBoundingClientRect();
        return a.left < b.right - 2 && a.right > b.left + 2 && a.top < b.bottom - 2 && a.bottom > b.top + 2;
      });
    }).length;
  return {
    pageOverflow,
    bareTables,
    tallRows,
    clippedTooltips,
    scrollWrappers,
    clippedCells,
    nakedOverflowCells,
    escapedProblemCells,
    missingArrowMarkers,
    leakLabelOverlaps,
  };
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

const checkTooltipPlacement = async (surface) => {
  const handles = await surface.$$("[data-tip]");
  let checked = 0;
  for (const handle of handles) {
    const box = await handle.evaluate((node) => {
      const rect = node.getBoundingClientRect();
      const x = rect.left + rect.width / 2;
      const y = rect.top + rect.height / 2;
      return {
        x,
        y,
        visible: rect.width > 0 && rect.height > 0 && x >= 0 && x < window.innerWidth && y >= 0 && y < window.innerHeight,
      };
    });
    if (!box.visible) {
      continue;
    }
    await handle.hover();
    const appeared = await surface.waitForFunction(
      () => document.querySelector(".jh-tooltip.is-visible") !== null,
      null,
      { timeout: 500 },
    ).then(() => true, () => false);
    if (!appeared) {
      return "подсказка не появилась";
    }
    await surface.waitForTimeout(120);
    const issue = await surface.evaluate(() => {
      const tip = document.querySelector(".jh-tooltip.is-visible");
      if (!tip) return "подсказка исчезла до проверки";
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
    if (checked >= 12) {
      break;
    }
  }
  return checked > 0 ? "" : "";
};

const checkFragmentNavigation = async (page, frame) => {
  const expectedFrameCount = page.frames().length;
  const hrefs = await frame.$$eval('a[href^="#"]', (anchors) =>
    Array.from(new Set(anchors.map((anchor) => anchor.getAttribute("href")).filter(Boolean))),
  );
  const issues = [];
  for (const href of hrefs) {
    let targetID = href.slice(1);
    try { targetID = decodeURIComponent(targetID); } catch (_) {}
    const targetExists = targetID === "" || await frame.evaluate((id) =>
      document.getElementById(id) !== null || document.getElementsByName(id).length > 0,
    targetID);
    if (!targetExists) {
      issues.push(`${href}: целевая секция отсутствует`);
      continue;
    }
    await frame.evaluate((value) => {
      const anchor = Array.from(document.querySelectorAll('a[href^="#"]'))
        .find((candidate) => candidate.getAttribute("href") === value);
      anchor?.click();
    }, href);
    await page.waitForTimeout(20);
    const state = await frame.evaluate((id) => {
      const target = id ? (document.getElementById(id) || document.getElementsByName(id)[0]) : document.documentElement;
      const rect = target?.getBoundingClientRect();
      return {
        documentURL: window.location.href,
        nestedShell: document.body.hasAttribute("data-jankhunter-single-html"),
        targetVisible: Boolean(rect && rect.bottom > 0 && rect.top < window.innerHeight),
      };
    }, targetID);
    if (state.nestedShell || !state.documentURL.startsWith("about:srcdoc")) {
      issues.push(`${href}: ссылка загрузила контейнер отчета внутрь iframe (${state.documentURL})`);
      break;
    }
    if (!state.targetVisible) {
      issues.push(`${href}: целевая секция не появилась в viewport после клика`);
    }
    if (page.frames().length !== expectedFrameCount) {
      issues.push(`${href}: число iframe изменилось с ${expectedFrameCount - 1} до ${page.frames().length - 1}`);
      break;
    }
  }
  return issues;
};

try {
  for (const viewport of viewports) {
    const page = await browser.newPage({ viewport, deviceScaleFactor: 1 });
    for (const report of reportPaths) {
      const reportName = `${report.set}-${report.type}`;
      const reportURL = pathToFileURL(report.path);
      reportURL.hash = `page=${report.page}`;
      await page.goto(reportURL.href, { waitUntil: "load" });
      const frameElement = await page.waitForSelector(`iframe.report-frame.active[data-page="${report.page}"]`);
      const reportFrame = await frameElement.contentFrame();
      if (!reportFrame) {
        throw new Error(`В snapshot-наборе ${reportName} не загрузилась встроенная страница ${report.page}`);
      }
      await reportFrame.waitForLoadState("load");
      await page.addStyleTag({ content: visualStabilityCSS });
      await reportFrame.addStyleTag({ content: visualStabilityCSS });
      await reportFrame.evaluate(() => document.fonts?.ready);
      await page.waitForTimeout(120);
      const issues = await collectLayoutIssues(reportFrame);
      const longCellToggle = await checkLongCellToggle(reportFrame);
      const zeroToggle = await checkZeroToggle(reportFrame);
      const tooltipIssue = await checkTooltipPlacement(reportFrame);
      const fragmentIssues = viewport.name === "desktop" && report.set === "short"
        ? await checkFragmentNavigation(page, reportFrame)
        : [];

      const displayName = reportName;

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
      if (issues.escapedProblemCells.length > 0) {
        failures.push(`${viewport.name}/${displayName}: содержимое problem/leak таблицы вышло за границы ячейки: ${JSON.stringify(issues.escapedProblemCells.slice(0, 3))}`);
      }
      if (issues.missingArrowMarkers > 0) {
        failures.push(`${viewport.name}/${displayName}: у ${issues.missingArrowMarkers} SVG-связей отсутствует рабочий marker-end`);
      }
      if (issues.leakLabelOverlaps > 0) {
        failures.push(`${viewport.name}/${displayName}: ${issues.leakLabelOverlaps} подписей связей перекрывают карточки графа утечек`);
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
      for (const issue of fragmentIssues) {
        failures.push(`${viewport.name}/${displayName}: ${issue}`);
      }

      await page.screenshot({
        path: resolve(outDir, `${reportName}-${viewport.name}.png`),
        fullPage: true,
      });

      if (report.page === "overview") {
        const mathLink = await reportFrame.$('a[href$="-math.html"]');
        if (!mathLink) {
          failures.push(`${viewport.name}/${displayName}: в обзоре нет ссылки на математический анализ`);
        } else {
          await mathLink.click();
          const routed = await page.waitForSelector('iframe.report-frame.active[data-page="math"]', { timeout: 1000 })
            .then(() => true, () => false);
          if (!routed) {
            failures.push(`${viewport.name}/${displayName}: внутренняя ссылка не переключила вкладку математического анализа`);
          }
        }
      }
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
