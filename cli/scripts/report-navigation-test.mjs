import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, realpathSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";

const cliRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const output = realpathSync(mkdtempSync(resolve(tmpdir(), "jh-report-navigation-")));
const sources = ["sample.jhlog", "release, candidate.jhlog", "source <file> & data.jhlog"].map((name) => resolve(output, name));
const run = (command, args) => {
  const result = spawnSync(command, args, { cwd: cliRoot, encoding: "utf8", env: { ...process.env, JH_NAV_OUT: output } });
  assert.equal(result.status, 0, `${command} ${args.join(" ")}\n${result.stdout}\n${result.stderr}`);
};

// Check the heading position, not just visibility of a potentially very tall section.
const waitForTarget = async (frame, hash) => {
  await frame.waitForFunction(async (fragment) => {
    await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const target = document.getElementById(decodeURIComponent(fragment.slice(1)));
    if (!target) return false;
    for (let details = target.closest("details"); details; details = details.parentElement?.closest("details")) {
      if (!details.open) return false;
    }
    const top = target.getBoundingClientRect().top;
    const navBottom = document.querySelector(".nav")?.getBoundingClientRect().bottom || 0;
    const atEnd = Math.abs(window.scrollY + window.innerHeight - document.documentElement.scrollHeight) <= 2;
    return top >= navBottom - 1 && top < window.innerHeight &&
      (Math.abs(top - navBottom - 12) <= 2 || atEnd);
  }, hash, { timeout: 5000 });
};

const checkMenu = async (page, frame, bundled) => {
  await frame.waitForSelector(".nav a");
  const hrefs = await frame.locator('.nav a[href^="#"]').evaluateAll((links) => links.map((link) => link.hash));
  assert.ok(hrefs.length > 0, "The report must have section links");
  const frameCount = page.frames().length;
  for (const hash of [...hrefs, hrefs[0], hrefs[0]]) {
    // Move away and close disclosures so repeated clicks cannot pass by coincidence.
    await frame.evaluate(() => {
      document.querySelectorAll("details[open]").forEach((details) => { details.open = false; });
      window.scrollTo({ top: 0, behavior: "instant" });
    });
    const link = frame.locator(`.nav a[href="${hash}"]`);
    await link.click();
    try {
      await waitForTarget(frame, hash);
    } catch (error) {
      const position = await frame.evaluate((fragment) => ({
        top: document.getElementById(fragment.slice(1))?.getBoundingClientRect().top,
        navBottom: document.querySelector(".nav")?.getBoundingClientRect().bottom,
        viewport: window.innerHeight,
        scrollY: window.scrollY,
      }), hash);
      throw new Error(`Navigation failed: ${bundled ? "bundle" : "standalone"} ${hash} ${JSON.stringify(position)}`, { cause: error });
    }
    assert.equal(await link.getAttribute("aria-current"), "location", hash);
    assert.equal(page.frames().length, frameCount, "A section link must not embed another report");
    if (bundled) assert.equal(frame.url(), "about:srcdoc");
    else assert.equal(new URL(frame.url()).hash, hash);
  }
  // Keyboard activation must take the same navigation route.
  const last = hrefs.at(-1);
  await frame.locator(`.nav a[href="${last}"]`).focus();
  await page.keyboard.press("Enter");
  await waitForTarget(frame, last);
  return hrefs.length;
};

const checkSourceRows = async (frame) => {
  const rows = frame.locator(".hero .report-source");
  assert.deepEqual((await rows.allTextContents()).sort(), [...sources].sort(), "One intact path per row");
  const layout = await frame.evaluate(() => {
    const rows = [...document.querySelectorAll(".hero .report-source")].map((row) => row.getBoundingClientRect());
    return {
      separated: rows.every((row, index) => index === 0 || row.top >= rows[index - 1].bottom),
      dateBelow: document.querySelector(".report-created").getBoundingClientRect().top >= rows.at(-1).bottom,
      overflow: document.documentElement.scrollWidth - window.innerWidth,
    };
  });
  assert.ok(layout.separated && layout.dateBelow, "Source paths and creation date must have separate rows");
  assert.ok(layout.overflow <= 1, `Report overflows horizontally by ${layout.overflow}px`);
};

let browser;
try {
  run("go", ["test", "./cmd/jankhunter", "-run", "^TestReportNavigationFixture$", "-count=1"]);
  const inspect = resolve(output, "inspect.html");
  const compare = resolve(output, "compare.html");
  browser = await chromium.launch();
  for (const width of process.env.JH_NAV_WIDTHS?.split(",").map(Number) || [1440, 768, 390]) {
    const page = await browser.newPage({ viewport: { width, height: 1000 } });
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.message));
    for (const path of [inspect, compare]) {
      await page.goto(pathToFileURL(path).href);
      for (const name of ["overview", "math", "leaks"]) {
        await page.locator(`.report-tab[data-page="${name}"]`).click();
        const element = await page.waitForSelector(`iframe.report-frame.active[data-page="${name}"]`);
        const frame = await element.contentFrame();
        await frame.waitForLoadState("load");
        if (path === inspect && name === "overview") await checkSourceRows(frame);
        const count = await checkMenu(page, frame, true);
        assert.deepEqual(errors, [], "Report JavaScript errors");
        console.log(`${width}px ${path === inspect ? "inspect" : "compare"}/${name}: ${count} links passed`);
        if (path === inspect && name === "overview") {
          const standalone = resolve(output, "standalone.html");
          writeFileSync(standalone, await frame.content());
        }
      }
    }
    await page.goto(pathToFileURL(resolve(output, "standalone.html")).href);
    await checkSourceRows(page.mainFrame());
    await checkMenu(page, page.mainFrame(), false);
    await page.goBack();
    await waitForTarget(page.mainFrame(), new URL(page.url()).hash);
    assert.deepEqual(errors, [], "Standalone JavaScript errors");
    console.log(`${width}px standalone: menu and history passed`);
    await page.close();
  }
} finally {
  await browser?.close();
  rmSync(output, { recursive: true, force: true });
}
