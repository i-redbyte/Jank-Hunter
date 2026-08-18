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

const buildGrowthReport = () => {
  const directory = resolve(outDir, "growth");
  const reportPath = resolve(directory, "inspect.html");
  mkdirSync(directory, { recursive: true });
  const result = spawnSync(
    "go",
    ["test", "./internal/report", "-run", "^TestWriteLogGrowthVisualFixture$", "-count=1"],
    {
      cwd: cliRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, JH_GROWTH_VISUAL_OUT: reportPath },
    },
  );
  if (result.status !== 0) {
    throw new Error(`Не удалось создать отчет роста журналов\n${result.stdout}\n${result.stderr}`);
  }
  return reportPath;
};

const buildDeferredSearchReport = () => {
  const directory = resolve(outDir, "deferred-search");
  const reportPath = resolve(directory, "inspect.html");
  mkdirSync(directory, { recursive: true });
  const result = spawnSync(
    "go",
    ["test", "./internal/report", "-run", "^TestCodeProblemReportKeepsHighCardinalityRegistryInCompressedArchive$", "-count=1"],
    {
      cwd: cliRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env, JH_DEFERRED_SEARCH_OUT: reportPath },
    },
  );
  if (result.status !== 0) {
    throw new Error(`Не удалось создать отчет для проверки поиска\n${result.stdout}\n${result.stderr}`);
  }
  return reportPath;
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
  const classGraphPath = resolve(setDir, "class-graph.jsonl");
  const classGraphRecords = [
    { format: 1, class: "CheckoutPresenter", edges: [{ caller: "submit()V", calleeClass: "com.app.checkout.MiddleConnector", calleeMethod: "dispatch()V", count: 4 }] },
    { format: 1, class: "com.app.checkout.MiddleConnector", edges: [{ caller: "dispatch()V", calleeClass: "com.app.checkout.CheckoutRepository", calleeMethod: "load()V", count: 4 }] },
    { format: 1, class: "com.app.checkout.CheckoutButton", edges: [{ caller: "click()V", calleeClass: "com.app.checkout.CheckoutRepository", calleeMethod: "load()V", count: 8 }] },
    { format: 1, class: "com.app.checkout.CheckoutRepository", edges: [{ caller: "load()V", calleeClass: "com.app.network.CheckoutApi", calleeMethod: "request()V", count: 5 }] },
    { format: 1, class: "com.app.analytics.StaticTracker", edges: [{ caller: "track()V", calleeClass: "com.app.analytics.StaticSink", calleeMethod: "send()V", count: 3 }] },
  ];
  for (let index = 0; index < 96; index += 1) {
    const suffix = String(index).padStart(3, "0");
    const next = String(index + 1).padStart(3, "0");
    classGraphRecords.push({
      format: 1,
      class: `com.production.feature${suffix}.subsystem.ClassWithVeryLongProductionName${suffix}`,
      edges: index < 95 ? [{
        caller: `executeFeature${suffix}()V`,
        calleeClass: `com.production.feature${next}.subsystem.ClassWithVeryLongProductionName${next}`,
        calleeMethod: `executeFeature${next}()V`,
        count: 96 - index,
      }] : [],
    });
  }
  writeFileSync(classGraphPath, classGraphRecords.map((record) => JSON.stringify(record)).join("\n") + "\n");
  const classGraphArgs = ["--class-graph", classGraphPath];
  const heapEvidencePath = resolve(setDir, "heap-evidence.json");
  writeFileSync(heapEvidencePath, JSON.stringify({
    sources: ["visual-fixture"],
    leaks: [{
      class_name: "com.app.checkout.CheckoutActivity",
      holder: "com.app.checkout.CheckoutPresenter",
      holder_field: "screen",
      gc_root: "android.app.ActivityThread",
      retained_size_kb: 4096,
      retained_object_count: 2,
      reference_path: [
        { class_name: "android.app.ActivityThread", field_name: "mActivities", kind: "gc_root" },
        { class_name: "com.app.checkout.CheckoutPresenter", field_name: "screen", kind: "instance_field" },
        { class_name: "com.app.checkout.CheckoutActivity", kind: "target" },
      ],
      confidence: "high",
    }],
  }, null, 2));
  const heapInspectArgs = ["--heap-evidence", heapEvidencePath];
  const heapCompareArgs = ["--baseline-heap-evidence", heapEvidencePath, "--candidate-heap-evidence", heapEvidencePath];
  const ownerMapArgs = [];
  if (presentation) {
    const ownerMapPath = resolve(setDir, "owner-map.json");
    const ownerRecords = [
      {
        format: 4,
        kind: "metadata",
        symbolNamespace: "00112233445566778899aabbccddeeff",
      },
      {
        format: 4,
        kind: "entry",
        id: "stable:0x0000000000001001",
        owner: "registration.ui.RegistrationActivity ru.mail.instantmessenger.flat.main.MainActivity __jh_dictionary_overflow__ click",
      },
      {
        format: 4,
        kind: "entry",
        id: "stable:0x0000000000001002",
        owner: "lifecycle.destroyed.ru.mail.instantmessenger.flat.main.MainActivity",
      },
      {
        format: 4,
        kind: "entry",
        id: "stable:0x0000000000001003",
        owner: "ru.mail.instantmessenger.flat.main.MainActivity.render.__jh_dictionary_overflow__.bind",
      },
    ];
    writeFileSync(ownerMapPath, ownerRecords.map((record) => JSON.stringify(record)).join("\n") + "\n");
    ownerMapArgs.push("--owner-map", ownerMapPath);
  }
  run(["inspect", ...logs, ...ownerMapArgs, ...diagnosticsArgs, ...classGraphArgs, ...heapInspectArgs, ...presentationFlag, "--out", inspectPath]);
  run([
    "compare",
    "--baseline", logs.join(","),
    "--candidate", candidateLogs.join(","),
    ...ownerMapArgs,
    ...diagnosticsArgs,
    ...classGraphArgs,
    ...heapCompareArgs,
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
  if (name === "short") {
    reports.push(
      { set: name, type: "inspect-influence-packages", path: inspectPath, page: "influence", influenceScenario: "packages" },
      { set: name, type: "inspect-influence-neighborhood", path: inspectPath, page: "influence", influenceScenario: "neighborhood" },
      { set: name, type: "inspect-influence-detail", path: inspectPath, page: "influence", influenceScenario: "detail" },
      { set: name, type: "inspect-influence-context", path: inspectPath, page: "influence", influenceScenario: "context" },
      { set: name, type: "inspect-influence-empty", path: inspectPath, page: "influence", influenceScenario: "empty" },
      { set: name, type: "inspect-influence-zoom", path: inspectPath, page: "influence", influenceScenario: "zoom" },
    );
  }
  if (presentation) {
    reports.push({ set: name, type: "inspect-section-overview", path: inspectPath, page: "math", section: "section-overview" });
    reports.push({ set: name, type: "inspect-method-reference", path: inspectPath, page: "math", section: "method-reference" });
    reports.push(
      { set: name, type: "readme-inspect-hero", path: inspectPath, page: "overview", readme: true },
      { set: name, type: "readme-inspect-signals", path: inspectPath, page: "overview", section: "overview", readme: true },
      { set: name, type: "readme-inspect-flows", path: inspectPath, page: "overview", section: "flows", readme: true },
      { set: name, type: "readme-leaks-explorer", path: inspectPath, page: "leaks", section: "summary", readme: true },
      { set: name, type: "readme-math-summary", path: inspectPath, page: "math", section: "math-overview", readme: true },
      { set: name, type: "readme-math-network-loops", path: inspectPath, page: "math", section: "network-loops", openDetails: true, readme: true },
      { set: name, type: "readme-math-integral", path: inspectPath, page: "math", section: "integral", openDetails: true, readme: true },
      { set: name, type: "readme-math-markov", path: inspectPath, page: "math", section: "markov", openDetails: true, readme: true },
      { set: name, type: "readme-math-relations-graph", path: inspectPath, page: "math", section: "graph", openDetails: true, readme: true },
      { set: name, type: "readme-influence-graph", path: inspectPath, page: "influence", section: "graph", influenceScenario: "context", readme: true },
      { set: name, type: "readme-diagnostics-overview", path: inspectPath, page: "diagnostics", section: "overview", readme: true },
      { set: name, type: "readme-compare-overview", path: comparePath, page: "overview", section: "compare", readme: true },
    );
  }

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
  {
    set: "deferred-search",
    type: "inspect",
    path: buildDeferredSearchReport(),
    page: "overview",
    plain: true,
    searchQuery: "ArchivedProblem074",
  },
  {
    set: "growth",
    type: "calendar-month",
    path: buildGrowthReport(),
    page: "overview",
    section: "log-growth",
    openDetails: true,
    growthPeriod: "current-month",
    plain: true,
    readme: true,
  },
];

const browser = await chromium.launch();
const viewports = [
  { name: "desktop", width: 1440, height: 1000 },
  { name: "mobile", width: 390, height: 844 },
  { name: "readme", width: 1280, height: 720, readmeOnly: true },
];
const visualStabilityCSS = `
  *, *::before, *::after {
    animation: none !important;
    scroll-behavior: auto !important;
    transition: none !important;
  }
`;
const failures = [];

const checkCodeProblemEvidence = async (frame) => {
  const details = await frame.$(".code-problem-details[data-code-problem-evidence-key]");
  if (!details) return { available: false };
  await details.evaluate((element) => { element.open = true; });
  const loaded = await frame.waitForFunction(
    () => document.querySelector(".code-problem-details[data-evidence-loaded='true']"),
    null,
    { timeout: 2000 },
  ).then(() => true, () => false);
  if (!loaded) return { available: true, loaded: false, signals: 0, drilldowns: 0 };
  const result = await details.evaluate((element) => ({
    available: true,
    loaded: true,
    signals: element.querySelectorAll(".problem-signal").length,
    drilldowns: element.querySelectorAll(".problem-drill").length,
    error: element.textContent.includes("Не удалось прочитать полные доказательства"),
  }));
  await details.evaluate((element) => { element.open = false; });
  return result;
};

const checkProblemSearch = async (frame, query) => frame.evaluate(async (searchQuery) => {
  const search = document.querySelector("[data-problem-search]");
  const feedback = document.querySelector("[data-problem-search-feedback]");
  const results = document.querySelector("[data-problem-search-results]");
  const scope = document.querySelector("[data-problem-card-scope]");
  if (!search || !feedback || !results || !scope) {
    return ["элементы поиска отсутствуют"];
  }
  const issues = [];
  const liveRows = Array.from(document.querySelectorAll("[data-code-problem-row]"));
  if (liveRows.some((row) => row.textContent.includes(searchQuery))) {
    issues.push("хвостовая строка уже находилась в DOM до поиска");
  }
  if (!scope.textContent.includes("все классы и строки подробностей")) {
    issues.push("область поиска не объясняет охват подробных строк");
  }
  search.value = searchQuery;
  search.dispatchEvent(new Event("input", { bubbles: true }));
  const waitUntil = async (predicate, attempts = 180) => {
    for (let index = 0; index < attempts; index += 1) {
      if (predicate()) return true;
      await new Promise((resolveTick) => requestAnimationFrame(resolveTick));
    }
    return false;
  };
  const indexed = await waitUntil(() => !feedback.textContent.includes("Ищу по всем строкам"));
  if (!indexed) {
    issues.push("индекс отложенных строк не завершился");
    return issues;
  }
  const resultButton = results.querySelector("button");
  if (!resultButton || !results.textContent.includes(searchQuery)) {
    issues.push("класс из отложенной строки не появился в результатах");
    return issues;
  }
  resultButton.click();
  const revealed = await waitUntil(() => {
    const target = document.querySelector(".report-search-highlight");
    return target?.textContent.includes(searchQuery);
  });
  if (!revealed) {
    issues.push("переход не материализовал и не подсветил найденную строку");
  }
  const target = document.querySelector(".report-search-highlight");
  let details = target?.closest("details");
  while (details) {
    if (!details.open) issues.push("родительский раздел найденной строки остался закрыт");
    details = details.parentElement?.closest("details");
  }
  document.querySelector("[data-problem-search-clear]")?.click();
  return issues;
}, query);

const collectLayoutIssues = async (page) => page.evaluate(() => {
  const root = document.documentElement;
  const pageOverflow = Math.max(0, root.scrollWidth - root.clientWidth);
  const bareTables = Array.from(document.querySelectorAll("table"))
    .filter((table) => !table.closest(".table-scroll"))
    .map((table) => table.textContent.trim().slice(0, 120));
  const tallRows = Array.from(document.querySelectorAll("tr"))
    .map((row) => ({
      height: row.getBoundingClientRect().height,
      top: row.getBoundingClientRect().top,
      bottom: row.getBoundingClientRect().bottom,
      text: row.textContent.trim().replace(/\s+/g, " ").slice(0, 160),
    }))
    .filter((row) => row.bottom > -200 && row.top < window.innerHeight + 200)
    .filter((row) => row.height > 180 && row.text.length > 0);
  const looseTableCells = Array.from(document.querySelectorAll("th, td"))
    .filter((cell) => {
      const style = getComputedStyle(cell);
      return parseFloat(style.paddingTop) > 8.1 ||
        parseFloat(style.paddingRight) > 8.1 ||
        parseFloat(style.paddingBottom) > 8.1 ||
        parseFloat(style.paddingLeft) > 8.1;
    })
    .map((cell) => cell.textContent.trim().replace(/\s+/g, " ").slice(0, 120));
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
  const escapedScenarioContent = Array.from(document.querySelectorAll(".scenario-insight-card, .ui-screen-insight, .ui-cause-card"))
    .flatMap((card) => {
      const cardRect = card.getBoundingClientRect();
      if (cardRect.width <= 0 || cardRect.height <= 0) return [];
      return Array.from(card.querySelectorAll("h3, h5, p, dt, dd, strong, small, span"))
        .filter((node) => {
          const rect = node.getBoundingClientRect();
          return rect.width > 0 && (
            rect.left < cardRect.left - 2 ||
            rect.right > cardRect.right + 2
          );
        })
        .map((node) => node.textContent.trim().replace(/\s+/g, " ").slice(0, 160));
    });
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
  const influenceTextOverflow = Array.from(document.querySelectorAll(".influence-node"))
    .flatMap((node) => {
      const card = node.querySelector(":scope > .node-card");
      if (!card) return [];
      const cardRect = card.getBoundingClientRect();
      return Array.from(node.querySelectorAll(":scope > text"))
        .filter((text) => {
          const rect = text.getBoundingClientRect();
          return rect.width > 0 && (
            rect.left < cardRect.left - 1 ||
            rect.right > cardRect.right + 1 ||
            rect.top < cardRect.top - 1 ||
            rect.bottom > cardRect.bottom + 1
          );
        })
        .map((text) => text.textContent.trim().slice(0, 120));
    });
  const textOverflow = Array.from(document.querySelectorAll("p, small, .section-status, .method-kind, .explain"))
    .filter((node) => {
      const rect = node.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && node.scrollWidth > node.clientWidth + 3;
    })
    .map((node) => node.textContent.trim().replace(/\s+/g, " ").slice(0, 120));
  const methodReferenceTypography = Array.from(document.querySelectorAll(".method-reference-card > summary"))
    .map((summary) => {
      const title = summary.querySelector(":scope > span:first-child");
      const kind = summary.querySelector(":scope > .method-kind");
      if (!title || !kind) return null;
      const titleStyle = getComputedStyle(title);
      const kindStyle = getComputedStyle(kind);
      const titleLineHeight = parseFloat(titleStyle.lineHeight);
      const kindLineHeight = parseFloat(kindStyle.lineHeight);
      return {
        title: title.textContent.trim(),
        titleFont: parseFloat(titleStyle.fontSize),
        titleLines: titleLineHeight > 0 ? title.getBoundingClientRect().height / titleLineHeight : 0,
        kindFont: parseFloat(kindStyle.fontSize),
        kindLines: kindLineHeight > 0 ? kind.getBoundingClientRect().height / kindLineHeight : 0,
      };
    })
    .filter((item) => item && (item.titleFont > 18 || item.kindFont > 12 || item.titleLines > 2.6 || item.kindLines > 2.6));
  const sectionOverviewLayout = Array.from(document.querySelectorAll(".section-overview-card"))
    .map((card) => {
      const title = card.querySelector(".section-overview-title > span:first-child");
      const status = card.querySelector(".section-overview-title > .section-status");
      const summary = card.querySelector(".section-overview-summary");
      if (!title || !status || !summary) return null;
      const cardRect = card.getBoundingClientRect();
      const titleRect = title.getBoundingClientRect();
      const statusRect = status.getBoundingClientRect();
      const summaryRect = summary.getBoundingClientRect();
      return {
        title: title.textContent.trim(),
        width: cardRect.width,
        titleStatusGap: statusRect.top - titleRect.bottom,
        statusSummaryGap: summaryRect.top - statusRect.bottom,
        alignContent: getComputedStyle(card).alignContent,
      };
    })
    .filter((item) => item && (
      (window.innerWidth >= 900 && item.width < 320) ||
      item.titleStatusGap > 18 ||
      item.statusSummaryGap > 20 ||
      item.alignContent !== "start"
    ));
  const gaugeIssues = Array.from(document.querySelectorAll(".gauge-card"))
    .map((card, index) => {
      const gauge = card.querySelector(".gauge");
      const ring = card.querySelector("svg.gauge-ring");
      const circles = Array.from(card.querySelectorAll("svg.gauge-ring circle"));
      if (!gauge || !ring || circles.length !== 2) {
        return { index, reason: "неполная SVG-структура" };
      }
      const cardRect = card.getBoundingClientRect();
      const gaugeRect = gauge.getBoundingClientRect();
      const ringRect = ring.getBoundingClientRect();
      const isContained = [gaugeRect, ringRect].every((rect) =>
        rect.left >= cardRect.left - 1 && rect.right <= cardRect.right + 1 &&
        rect.top >= cardRect.top - 1 && rect.bottom <= cardRect.bottom + 1,
      );
      if (!isContained || card.scrollWidth > card.clientWidth + 1 || gauge.scrollWidth > gauge.clientWidth + 1) {
        return { index, reason: "выход за границы карточки" };
      }
      const invalidCircle = circles.some((circle) =>
        circle.getAttribute("cx") !== "60" || circle.getAttribute("cy") !== "60" ||
        circle.getAttribute("r") !== "48" || circle.getAttribute("pathLength") !== "100",
      );
      if (invalidCircle || ring.getAttribute("viewBox") !== "0 0 120 120") {
        return { index, reason: "ненормализованная геометрия" };
      }
      const valueStyle = getComputedStyle(circles[1]);
      if (valueStyle.strokeDasharray === "none" || parseFloat(valueStyle.strokeWidth) <= 0) {
        return { index, reason: "не задана дуга значения" };
      }
      return null;
    })
    .filter(Boolean);
  return {
    pageOverflow,
    bareTables,
    tallRows,
    looseTableCells,
    clippedTooltips,
    scrollWrappers,
    clippedCells,
    nakedOverflowCells,
    escapedProblemCells,
    escapedScenarioContent,
    missingArrowMarkers,
    leakLabelOverlaps,
    influenceTextOverflow,
    textOverflow,
    methodReferenceTypography,
    sectionOverviewLayout,
    gaugeIssues,
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

const checkGrowthPeriod = async (frame, period) => frame.evaluate(async (selectedPeriod) => {
  const panel = document.querySelector("[data-log-growth]");
  if (!panel) return ["раздел роста журналов отсутствует"];
  const issues = [];
  const periodButton = panel.querySelector(`[data-growth-period="${selectedPeriod}"]`);
  const calculateButton = panel.querySelector("[data-growth-calculate]");
  if (!periodButton || !calculateButton) return ["кнопки расчета периода отсутствуют"];
  periodButton.click();
  calculateButton.click();
  await new Promise((resolveTick) => requestAnimationFrame(() => resolveTick()));
  const value = (name) => panel.querySelector(`[data-growth-value="${name}"]`)?.textContent.trim() || "";
  const expected = {
    sessions: "31",
    duration: "31 мин",
    generated: "39,5 МиБ",
    retained: "1 МиБ",
    fill: "100% лимита",
    reached: "6 сессий достигли лимита",
    "limit-reached": "21",
    rotations: "42",
    evicted: "21 МиБ",
  };
  for (const [name, expectedValue] of Object.entries(expected)) {
    if (value(name) !== expectedValue) issues.push(`${name}=${value(name)}, ожидалось ${expectedValue}`);
  }
  if (panel.querySelector("[data-growth-from]")?.value !== "01.07.2026" ||
      panel.querySelector("[data-growth-to]")?.value !== "31.07.2026") {
    issues.push("границы календарного месяца рассчитаны неверно");
  }
  if (panel.querySelectorAll("[data-growth-session-rows] tr").length !== 31) {
    issues.push("таблица сессий не содержит 31 строку");
  }
  if (panel.querySelectorAll("[data-growth-session-rows] tr.has-limit").length !== 6) {
    issues.push("в таблице неверно отмечены сессии с остановкой по лимиту");
  }
  if (panel.querySelector("[data-growth-period-result]")?.hidden) {
    issues.push("итог выбранного месяца остался скрыт");
  }
  return issues;
}, period);

const exerciseInfluenceScenario = async (frame, scenario) => frame.evaluate(async (selectedScenario) => {
  const root = document.querySelector("[data-influence-workbench]");
  if (!root) return { issues: ["influence workbench отсутствует"] };
  const issues = [];
  const tick = () => new Promise((resolveTick) => setTimeout(resolveTick, 0));
  const stateHash = () => window.location.hash + " " + window.parent.location.hash;
  const clickView = async (mode) => {
    const button = root.querySelector(`[data-influence-view="${mode}"]`);
    if (!button) {
      issues.push(`кнопка режима ${mode} отсутствует`);
      return;
    }
    button.click();
    await tick();
  };
  const uniqueIDs = new Set();
  for (const element of document.querySelectorAll("[id]")) {
    if (uniqueIDs.has(element.id)) issues.push(`duplicate DOM id ${element.id}`);
    uniqueIDs.add(element.id);
  }
  if (root.querySelectorAll("[data-influence-view]").length !== 5) issues.push("должно быть пять режимов данных");
  if (root.querySelectorAll(".influence-legend-item").length !== 6) issues.push("легенда неполная");
  if (!root.querySelector("[data-influence-shown]")?.textContent.includes("Показано")) issues.push("нет shown N of M");

  if (selectedScenario === "packages") {
    await clickView("packages");
    const depth = root.querySelector("[data-influence-package-depth]");
    depth.value = "5";
    depth.dispatchEvent(new Event("change", { bubbles: true }));
    await tick();
    if (root.querySelectorAll(".influence-node.aggregate").length < 80) issues.push("large package view содержит меньше 80 групп");
    const packageNode = root.querySelector(".influence-node.aggregate");
    packageNode?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await tick();
    if (!root.querySelector("[data-influence-expand-package]")) issues.push("package detail не раскрывается до классов");
    if (!stateHash().includes("view%3Dpackages") && !stateHash().includes("view=packages")) issues.push("package mode не сохранен в hash");
  }

  if (selectedScenario === "neighborhood") {
    await clickView("neighborhood");
    const search = root.querySelector("[data-influence-search]");
    search.value = "com.app.checkout.CheckoutRepository";
    search.dispatchEvent(new Event("change", { bubbles: true }));
    await tick();
    const direction = root.querySelector("[data-influence-direction]");
    direction.value = "both";
    direction.dispatchEvent(new Event("change", { bubbles: true }));
    const depth = root.querySelector("[data-influence-depth]");
    depth.value = "3";
    depth.dispatchEvent(new Event("change", { bubbles: true }));
    await tick();
    if (!root.querySelector("[data-influence-view-title]")?.textContent.includes("Окрестность")) issues.push("neighborhood view не активирован");
    if (!stateHash().includes("depth%3D3") && !stateHash().includes("depth=3")) issues.push("depth не сохранен в hash");
  }

  if (selectedScenario === "detail") {
    const hprofNode = root.querySelector(".influence-node.hprof") || root.querySelector(".influence-node");
    hprofNode?.focus();
    hprofNode?.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true }));
    await tick();
    if (!root.querySelector(".influence-detail h3")) issues.push("detail panel не открылся с клавиатуры");
    if (root.querySelector(".influence-node.hprof") && !root.querySelector(".influence-hprof-badge")) issues.push("HPROF evidence не показан в detail panel");
    if (!root.querySelector(".influence-detail-metrics")) issues.push("detail panel не содержит метрики");
  }

  if (selectedScenario === "context") {
    await clickView("context");
    if (root.querySelectorAll(".influence-node.connector").length === 0) issues.push("context view не показывает connector node");
    if (root.querySelectorAll(".influence-edge.evidence-static").length === 0) issues.push("context connector не сохранил static evidence");
  }

  if (selectedScenario === "empty") {
    const search = root.querySelector("[data-influence-search]");
    search.value = "__missing.production.Class__";
    search.dispatchEvent(new Event("change", { bubbles: true }));
    await tick();
    if (root.querySelectorAll(".influence-node").length !== 0) issues.push("empty search оставил узлы");
    if (root.querySelector("[data-influence-empty]")?.hidden) issues.push("empty state скрыт");
  }

  if (selectedScenario === "zoom") {
    const zoomIn = root.querySelector('[data-influence-zoom="in"]');
    const zoomOut = root.querySelector('[data-influence-zoom="out"]');
    for (let index = 0; index < 20; index += 1) zoomIn?.click();
    const transformAtMax = root.querySelector("[data-influence-stage]")?.getAttribute("transform") || "";
    for (let index = 0; index < 40; index += 1) zoomOut?.click();
    const transformAtMin = root.querySelector("[data-influence-stage]")?.getAttribute("transform") || "";
    const maxScale = Number((transformAtMax.match(/scale\(([^)]+)\)/) || [])[1]);
    const minScale = Number((transformAtMin.match(/scale\(([^)]+)\)/) || [])[1]);
    if (maxScale > 2.401 || minScale < 0.449) issues.push(`zoom вышел за границы ${minScale}..${maxScale}`);
    const viewport = root.querySelector("[data-influence-viewport]");
    if (getComputedStyle(viewport).overflow !== "hidden") issues.push("pan/zoom выходит за пределы graph viewport");
    root.querySelector("[data-influence-reset-filters]")?.click();
    await tick();
    if (!root.querySelector('[data-influence-view="problems"]')?.classList.contains("is-active")) issues.push("reset не вернул Problems view");
  }

  const mixedEdges = root.querySelectorAll(".influence-edge.evidence-mixed").length;
  const staticEdges = root.querySelectorAll(".influence-edge.evidence-static").length;
  const runtimeEdges = root.querySelectorAll(".influence-edge.evidence-runtime").length;
  return {
    issues,
    mixedEdges,
    staticEdges,
    runtimeEdges,
    pageOverflow: Math.max(0, document.documentElement.scrollWidth - document.documentElement.clientWidth),
  };
}, scenario);

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
    const pageErrors = [];
    page.on("pageerror", (error) => pageErrors.push(error.message));
    page.on("console", (message) => {
      if (message.type() === "error") pageErrors.push(message.text());
    });
    for (const report of reportPaths) {
      if (viewport.readmeOnly ? !report.readme : report.readme) continue;
      const errorStart = pageErrors.length;
      const reportName = `${report.set}-${report.type}`;
      const reportURL = pathToFileURL(report.path);
      reportURL.hash = `page=${report.page}`;
      await page.goto(reportURL.href, { waitUntil: "load" });
      let reportFrame;
      if (report.plain) {
        reportFrame = page.mainFrame();
      } else {
        const frameElement = await page.waitForSelector(`iframe.report-frame.active[data-page="${report.page}"]`);
        reportFrame = await frameElement.contentFrame();
        if (!reportFrame) {
          throw new Error(`В snapshot-наборе ${reportName} не загрузилась встроенная страница ${report.page}`);
        }
        await reportFrame.waitForLoadState("load");
      }
      await page.addStyleTag({ content: visualStabilityCSS });
      await reportFrame.addStyleTag({ content: visualStabilityCSS });
      await reportFrame.evaluate(() => document.fonts?.ready);
      await page.waitForTimeout(120);
      const influenceResult = report.page === "influence"
        ? await exerciseInfluenceScenario(reportFrame, report.influenceScenario || "default")
        : null;
      if (report.section) {
        await reportFrame.evaluate(({ id, openDetails }) => {
          const target = document.getElementById(id);
          if (openDetails) target?.querySelector(":scope > details.fold")?.setAttribute("open", "");
          target?.scrollIntoView({ block: "start" });
        }, { id: report.section, openDetails: Boolean(report.openDetails) });
        await page.waitForTimeout(80);
      }
      const growthIssues = report.growthPeriod
        ? await checkGrowthPeriod(reportFrame, report.growthPeriod)
        : [];
      const codeEvidence = report.page === "overview" || report.page === "math"
        ? await checkCodeProblemEvidence(reportFrame)
        : { available: false };
      const problemSearchIssues = report.searchQuery
        ? await checkProblemSearch(reportFrame, report.searchQuery)
        : [];
      const issues = await collectLayoutIssues(reportFrame);
      const longCellToggle = await checkLongCellToggle(reportFrame);
      const zeroToggle = await checkZeroToggle(reportFrame);
      const tooltipIssue = report.section ? "" : await checkTooltipPlacement(reportFrame);
      const fragmentIssues = viewport.name === "desktop" && report.set === "short"
        && !report.influenceScenario
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
      if (issues.looseTableCells.length > 0) {
        failures.push(`${viewport.name}/${displayName}: отступы таблиц превышают 8px: ${JSON.stringify(issues.looseTableCells.slice(0, 3))}`);
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
      if (issues.escapedScenarioContent.length > 0) {
        failures.push(`${viewport.name}/${displayName}: содержимое сценарной карточки вышло за границы: ${JSON.stringify(issues.escapedScenarioContent.slice(0, 3))}`);
      }
      if (issues.missingArrowMarkers > 0) {
        failures.push(`${viewport.name}/${displayName}: у ${issues.missingArrowMarkers} SVG-связей отсутствует рабочий marker-end`);
      }
      if (issues.leakLabelOverlaps > 0) {
        failures.push(`${viewport.name}/${displayName}: ${issues.leakLabelOverlaps} подписей связей перекрывают карточки графа утечек`);
      }
      if (issues.influenceTextOverflow.length > 0) {
        failures.push(`${viewport.name}/${displayName}: текст вышел за карточки графа влияния: ${JSON.stringify(issues.influenceTextOverflow.slice(0, 3))}`);
      }
      if (issues.textOverflow.length > 0) {
        failures.push(`${viewport.name}/${displayName}: текстовые блоки вышли за границы: ${JSON.stringify(issues.textOverflow.slice(0, 3))}`);
      }
      if (issues.methodReferenceTypography.length > 0) {
        failures.push(`${viewport.name}/${displayName}: типографика справки по методам слишком крупная: ${JSON.stringify(issues.methodReferenceTypography.slice(0, 3))}`);
      }
      if (issues.sectionOverviewLayout.length > 0) {
        failures.push(`${viewport.name}/${displayName}: карточки сводки разделов растянуты: ${JSON.stringify(issues.sectionOverviewLayout.slice(0, 3))}`);
      }
      if (issues.gaugeIssues.length > 0) {
        failures.push(`${viewport.name}/${displayName}: дефекты индикаторов здоровья: ${JSON.stringify(issues.gaugeIssues)}`);
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
      for (const issue of growthIssues) {
        failures.push(`${viewport.name}/${displayName}: ${issue}`);
      }
      if (codeEvidence.available && (!codeEvidence.loaded || codeEvidence.error || codeEvidence.signals === 0)) {
        failures.push(`${viewport.name}/${displayName}: полные доказательства строки кода не раскрылись (${JSON.stringify(codeEvidence)})`);
      }
      for (const issue of problemSearchIssues) {
        failures.push(`${viewport.name}/${displayName}: ${issue}`);
      }
      if (influenceResult) {
        for (const issue of influenceResult.issues) {
          failures.push(`${viewport.name}/${displayName}: ${issue}`);
        }
        if (!report.influenceScenario && (influenceResult.mixedEdges === 0 || influenceResult.staticEdges === 0)) {
          failures.push(`${viewport.name}/${displayName}: default view не различает mixed/static evidence`);
        }
        if (influenceResult.pageOverflow > 6) {
          failures.push(`${viewport.name}/${displayName}: influence scenario шире viewport на ${influenceResult.pageOverflow}px`);
        }
      }
      const newErrors = pageErrors.slice(errorStart);
      if (newErrors.length > 0) {
        failures.push(`${viewport.name}/${displayName}: JS errors: ${JSON.stringify(newErrors.slice(0, 3))}`);
      }

      await page.screenshot({
        path: resolve(outDir, `${reportName}-${viewport.name}.png`),
        fullPage: !report.section,
      });

      if (report.page === "overview" && !report.readme && !report.plain) {
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
