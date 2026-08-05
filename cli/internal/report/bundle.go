package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
)

// BundlePage is one self-contained report view embedded into a single HTML file.
// Href keeps the former companion filename so links already rendered inside report
// pages can be routed to the matching embedded view.
type BundlePage struct {
	ID    string
	Title string
	Href  string
	HTML  []byte
}

type encodedBundlePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Href  string `json:"href"`
	HTML  string `json:"html"`
}

const bundledPageBridge = `<script>
(function () {
  function scrollToFragment(href) {
    var fragment = href.slice(1);
    if (!fragment) {
      window.scrollTo({top: 0, left: 0});
      return;
    }
    try { fragment = decodeURIComponent(fragment); } catch (_) {}
    var target = document.getElementById(fragment);
    if (!target) {
      var named = document.getElementsByName(fragment);
      target = named.length > 0 ? named[0] : null;
    }
    if (target) target.scrollIntoView({block: "start"});
  }

  document.addEventListener("click", function (event) {
    var anchor = event.target.closest && event.target.closest("a[href]");
    if (!anchor || window.parent === window) return;
    var href = anchor.getAttribute("href") || "";
    if (!href) return;
    if (href.charAt(0) === "#") {
      event.preventDefault();
      scrollToFragment(href);
      return;
    }
    if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return;
    event.preventDefault();
    window.parent.postMessage({type: "jankhunter-report:navigate", href: href}, "*");
  }, true);
})();
</script>`

func WriteBundle(path string, pages []BundlePage) error {
	return WriteBundleWithOptions(path, pages, ReportOptions{})
}

func WriteBundleWithOptions(path string, pages []BundlePage, options ReportOptions) error {
	encoded, err := encodeBundlePages(pages)
	if err != nil {
		return err
	}
	prefix := modernSingleHTMLBundlePrefix
	if options.Style.normalized() == ReportStyleLegacy {
		prefix = legacySingleHTMLBundlePrefix
	}
	return atomicfile.Write(path, 0o644, func(file *os.File) error {
		if _, err := io.WriteString(file, prefix); err != nil {
			return fmt.Errorf("write report bundle shell: %w", err)
		}
		if _, err := file.Write(encoded); err != nil {
			return fmt.Errorf("write embedded report pages: %w", err)
		}
		if _, err := io.WriteString(file, singleHTMLBundleSuffix); err != nil {
			return fmt.Errorf("write report bundle runtime: %w", err)
		}
		return nil
	})
}

func encodeBundlePages(pages []BundlePage) ([]byte, error) {
	if len(pages) == 0 {
		return nil, fmt.Errorf("report bundle needs at least one page")
	}
	seenIDs := make(map[string]struct{}, len(pages))
	seenHrefs := make(map[string]struct{}, len(pages))
	encoded := make([]encodedBundlePage, 0, len(pages))
	for index, page := range pages {
		page.ID = strings.TrimSpace(page.ID)
		page.Title = strings.TrimSpace(page.Title)
		page.Href = strings.TrimSpace(page.Href)
		if page.ID == "" || page.Title == "" || page.Href == "" || len(page.HTML) == 0 {
			return nil, fmt.Errorf("report bundle page %d has an empty id, title, href, or document", index)
		}
		if _, exists := seenIDs[page.ID]; exists {
			return nil, fmt.Errorf("duplicate report bundle page id %q", page.ID)
		}
		if _, exists := seenHrefs[page.Href]; exists {
			return nil, fmt.Errorf("duplicate report bundle page href %q", page.Href)
		}
		seenIDs[page.ID] = struct{}{}
		seenHrefs[page.Href] = struct{}{}
		encoded = append(encoded, encodedBundlePage{
			ID:    page.ID,
			Title: page.Title,
			Href:  page.Href,
			HTML:  injectBundleBridge(string(page.HTML)),
		})
	}
	payload, err := json.Marshal(encoded)
	if err != nil {
		return nil, fmt.Errorf("encode report bundle pages: %w", err)
	}
	return payload, nil
}

func injectBundleBridge(document string) string {
	lower := strings.ToLower(document)
	if index := strings.LastIndex(lower, "</body>"); index >= 0 {
		return document[:index] + bundledPageBridge + document[index:]
	}
	return document + bundledPageBridge
}

const modernSingleHTMLBundlePrefix = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Jank Hunter · отчет</title>
  <style>
    :root { color-scheme: dark; --shell-bg: #06140b; --shell-panel: #0a1c11; --shell-line: rgba(151, 184, 155, .22); --shell-text: #edf4e9; --shell-muted: #91a698; --shell-accent: #a7c990; --shell-rail: #99af9c; --shell-rail-text: #102017; --shell-active: #21452e; }
    * { box-sizing: border-box; }
    html, body { width: 100%; height: 100%; margin: 0; overflow: hidden; background: var(--shell-bg); color: var(--shell-text); font-family: "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    button, input, select, textarea { font: inherit; }
    .report-shell { width: 100%; height: 100%; display: grid; grid-template-columns: 232px minmax(0, 1fr); grid-template-rows: minmax(0, 1fr); }
    .report-toolbar { display: flex; flex-direction: column; align-items: stretch; gap: 0; min-width: 0; padding: 28px 20px 22px; color: var(--shell-rail-text); background: var(--shell-rail); border-right: 1px solid rgba(16, 32, 23, .22); z-index: 2; }
    .report-brand { flex: 0 0 auto; padding: 0 6px 20px; border-bottom: 1px solid rgba(16, 32, 23, .24); font-family: Menlo, "SFMono-Regular", Consolas, monospace; font-size: 13px; font-weight: 500; letter-spacing: .06em; text-transform: uppercase; white-space: nowrap; }
    .report-logo { display: block; width: 100%; max-width: 184px; height: auto; max-height: 114px; object-fit: contain; object-position: left center; }
    .report-brand::after { content: "JHLOG v9"; display: block; margin-top: 8px; color: rgba(16, 32, 23, .64); font-size: 11px; letter-spacing: .08em; }
    .report-tabs { display: grid; gap: 6px; min-width: 0; margin-top: 24px; }
    .report-tabs::before { content: "РАЗДЕЛЫ ОТЧЁТА"; display: block; margin: 0 7px 6px; color: rgba(16, 32, 23, .64); font-family: Menlo, "SFMono-Regular", Consolas, monospace; font-size: 11px; letter-spacing: .08em; }
    .report-tab { width: 100%; min-width: 0; appearance: none; border: 0; border-radius: 4px; padding: 11px 10px; background: transparent; color: rgba(16, 32, 23, .72); font-weight: 400; text-align: left; overflow-wrap: anywhere; cursor: pointer; transition: background .16s ease, color .16s ease; }
    .report-tab:hover, .report-tab:focus-visible { color: var(--shell-rail-text); background: rgba(237, 244, 233, .24); outline: none; }
    .report-tab[aria-selected="true"] { color: var(--shell-text); background: var(--shell-active); }
    .report-frames { position: relative; min-width: 0; min-height: 0; background: var(--shell-bg); }
    .report-frame { display: none; width: 100%; height: 100%; border: 0; background: var(--shell-bg); }
    .report-frame.active { display: block; }
    .report-error { display: grid; place-items: center; height: 100%; padding: 24px; color: var(--shell-muted); text-align: center; }
    @media (max-width: 820px) {
      .report-shell { grid-template-columns: minmax(0, 1fr); grid-template-rows: auto minmax(0, 1fr); }
      .report-toolbar { padding: 16px 18px 14px; border-right: 0; border-bottom: 1px solid rgba(16, 32, 23, .22); }
      .report-brand { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 0 0 14px; }
      .report-logo { width: 132px; max-height: 80px; }
      .report-brand::after { display: block; margin: 0; }
      .report-tabs { grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 14px; }
      .report-tabs::before { grid-column: 1 / -1; }
      .report-tab { padding: 9px 10px; }
    }
    @media (max-width: 380px) {
      .report-tabs { grid-template-columns: minmax(0, 1fr); }
    }
  </style>
</head>
<body data-jankhunter-single-html data-report-style="modern">
  <main class="report-shell">
    <header class="report-toolbar">
      <div class="report-brand"><img class="report-logo" src="` + reportLogoDataURI + `" alt="Jank Hunter"></div>
      <nav class="report-tabs" role="tablist" aria-label="Разделы отчета"></nav>
    </header>
    <section class="report-frames" aria-live="polite"></section>
  </main>
  <script id="jankhunter-report-pages" type="application/json">`

const legacySingleHTMLBundlePrefix = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Jank Hunter · отчет</title>
  <style>
    :root { color-scheme: dark; --shell-bg: #080d18; --shell-panel: rgba(15, 23, 42, .96); --shell-line: rgba(148, 163, 184, .22); --shell-text: #e2e8f0; --shell-muted: #94a3b8; --shell-accent: #67e8f9; }
    * { box-sizing: border-box; }
    html, body { width: 100%; height: 100%; margin: 0; overflow: hidden; background: var(--shell-bg); color: var(--shell-text); font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    .report-shell { height: 100%; display: grid; grid-template-rows: auto minmax(0, 1fr); }
    .report-toolbar { display: flex; align-items: center; gap: 14px; min-width: 0; padding: 10px 14px; border-bottom: 1px solid var(--shell-line); background: var(--shell-panel); box-shadow: 0 8px 28px rgba(0, 0, 0, .24); z-index: 2; }
    .report-brand { flex: 0 0 auto; font-size: 13px; font-weight: 800; letter-spacing: .08em; text-transform: uppercase; white-space: nowrap; }
    .report-brand span { color: var(--shell-accent); }
    .report-tabs { display: flex; gap: 7px; min-width: 0; overflow-x: auto; scrollbar-width: thin; }
    .report-tab { flex: 0 0 auto; appearance: none; border: 1px solid var(--shell-line); border-radius: 999px; padding: 7px 12px; background: rgba(30, 41, 59, .72); color: var(--shell-muted); font: inherit; font-size: 13px; font-weight: 700; cursor: pointer; transition: border-color .16s ease, background .16s ease, color .16s ease; }
    .report-tab:hover { border-color: rgba(103, 232, 249, .5); color: var(--shell-text); }
    .report-tab[aria-selected="true"] { border-color: rgba(103, 232, 249, .7); background: rgba(8, 145, 178, .24); color: #ecfeff; }
    .report-frames { position: relative; min-height: 0; background: #050914; }
    .report-frame { display: none; width: 100%; height: 100%; border: 0; background: #050914; }
    .report-frame.active { display: block; }
    .report-error { display: grid; place-items: center; height: 100%; padding: 24px; color: var(--shell-muted); text-align: center; }
    @media (max-width: 640px) {
      .report-toolbar { align-items: stretch; flex-direction: column; gap: 8px; padding: 8px 10px; }
      .report-brand { padding-left: 2px; }
      .report-tab { padding: 6px 10px; font-size: 12px; }
    }
  </style>
</head>
<body data-jankhunter-single-html data-report-style="legacy">
  <main class="report-shell">
    <header class="report-toolbar">
      <div class="report-brand">Jank <span>Hunter</span></div>
      <nav class="report-tabs" role="tablist" aria-label="Разделы отчета"></nav>
    </header>
    <section class="report-frames" aria-live="polite"></section>
  </main>
  <script id="jankhunter-report-pages" type="application/json">`

const singleHTMLBundleSuffix = `</script>
  <script>
    (function () {
      "use strict";
      var tabs = document.querySelector(".report-tabs");
      var frames = document.querySelector(".report-frames");
      var pages;
      try {
        pages = JSON.parse(document.getElementById("jankhunter-report-pages").textContent);
      } catch (error) {
        frames.innerHTML = '<div class="report-error">Не удалось прочитать встроенные страницы отчета.</div>';
        return;
      }

      var pagesByID = new Map();
      var pagesByHref = new Map();
      var buttonsByID = new Map();
      var framesByID = new Map();

      function hrefKey(href) {
        var path = String(href || "").split(/[?#]/, 1)[0].replace(/\\/g, "/");
        try { path = decodeURIComponent(path); } catch (_) {}
        return path.slice(path.lastIndexOf("/") + 1);
      }

      function pageFromLocation() {
        var match = /^#(?:jh-)?page=([^&]+)$/.exec(window.location.hash);
        if (!match) return pages[0];
        try { return pagesByID.get(decodeURIComponent(match[1])) || pages[0]; }
        catch (_) { return pages[0]; }
      }

      function ensureFrame(page) {
        var frame = framesByID.get(page.id);
        if (frame) return frame;
        frame = document.createElement("iframe");
        frame.className = "report-frame";
        frame.id = "jankhunter-report-frame-" + page.id;
        frame.title = page.title;
        frame.dataset.page = page.id;
        frame.srcdoc = page.html;
        frames.appendChild(frame);
        framesByID.set(page.id, frame);
        return frame;
      }

      function showPage(page, updateLocation) {
        if (!page) return;
        framesByID.forEach(function (frame) { frame.classList.remove("active"); });
        buttonsByID.forEach(function (button) { button.setAttribute("aria-selected", "false"); });
        var frame = ensureFrame(page);
        frame.classList.add("active");
        var button = buttonsByID.get(page.id);
        button.setAttribute("aria-selected", "true");
        button.scrollIntoView({block: "nearest", inline: "nearest"});
        document.title = "Jank Hunter · " + page.title;
        if (updateLocation) history.replaceState(null, "", "#page=" + encodeURIComponent(page.id));
      }

      pages.forEach(function (page) {
        pagesByID.set(page.id, page);
        pagesByHref.set(hrefKey(page.href), page);
        var button = document.createElement("button");
        button.type = "button";
        button.className = "report-tab";
        button.dataset.page = page.id;
        button.textContent = page.title;
        button.setAttribute("role", "tab");
        button.setAttribute("aria-controls", "jankhunter-report-frame-" + page.id);
        button.setAttribute("aria-selected", "false");
        button.addEventListener("click", function () { showPage(page, true); });
        tabs.appendChild(button);
        buttonsByID.set(page.id, button);
      });

      window.addEventListener("message", function (event) {
        if (!event.data || event.data.type !== "jankhunter-report:navigate") return;
        var page = pagesByHref.get(hrefKey(event.data.href));
        if (page) showPage(page, true);
      });
      window.addEventListener("hashchange", function () { showPage(pageFromLocation(), false); });
      showPage(pageFromLocation(), false);
    })();
  </script>
</body>
</html>
`
