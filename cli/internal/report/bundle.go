package report

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/i-redbyte/jank-hunter/cli/internal/atomicfile"
)

// BundlePage is one self-contained report view embedded into a single HTML file.
// Href keeps the former companion filename so links already rendered inside report
// pages can be routed to the matching embedded view. Exactly one of HTML or Path
// must provide the document; Path lets production reports avoid retaining every
// generated page in memory while the bundle is written.
type BundlePage struct {
	ID    string
	Title string
	Href  string
	HTML  []byte
	Path  string
}

type encodedBundlePage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Href    string `json:"href"`
	Payload string `json:"payload"`
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
	manifest, err := encodeBundlePages(pages)
	if err != nil {
		return err
	}
	return atomicfile.Write(path, 0o644, func(file *os.File) error {
		if _, err := io.WriteString(file, singleHTMLBundlePrefix); err != nil {
			return fmt.Errorf("write report bundle shell: %w", err)
		}
		if err := json.NewEncoder(file).Encode(manifest); err != nil {
			return fmt.Errorf("write report page manifest: %w", err)
		}
		if _, err := io.WriteString(file, "</script>\n"); err != nil {
			return fmt.Errorf("close report page manifest: %w", err)
		}
		for index, page := range pages {
			if _, err := fmt.Fprintf(
				file,
				`  <script id="%s" type="application/octet-stream" data-jankhunter-report-payload data-encoding="gzip-base64">`,
				manifest[index].Payload,
			); err != nil {
				return fmt.Errorf("write report page %d payload header: %w", index, err)
			}
			if err := writeCompressedBundlePageSource(file, page); err != nil {
				return fmt.Errorf("write report page %d payload: %w", index, err)
			}
			if _, err := io.WriteString(file, "</script>\n"); err != nil {
				return fmt.Errorf("close report page %d payload: %w", index, err)
			}
		}
		if _, err := io.WriteString(file, singleHTMLBundleSuffix); err != nil {
			return fmt.Errorf("write report bundle runtime: %w", err)
		}
		return nil
	})
}

// writeCompressedBundlePage keeps a multi-view report small without discarding any table rows.
// The page is streamed through gzip and base64 directly into its inert script payload, avoiding an
// additional page-sized encoded buffer. DefaultCompression avoids carrying tens of thousands of
// repetitive table cells into the artifact while keeping generation cheap on developer and CI
// machines. For real multi-megabyte reports it saves materially more space than BestSpeed for only
// a fraction of a second of additional offline work.
func writeCompressedBundlePage(target io.Writer, document []byte) error {
	return writeCompressedBundlePageReader(
		target,
		bytes.NewReader(document),
		int64(lastASCIIFoldIndex(document, []byte("</body>"))),
	)
}

func writeCompressedBundlePageSource(target io.Writer, page BundlePage) error {
	if len(page.HTML) > 0 {
		return writeCompressedBundlePage(target, page.HTML)
	}
	path := strings.TrimSpace(page.Path)
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open report page %s: %w", path, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat report page %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		_ = file.Close()
		return fmt.Errorf("report page %s is not a non-empty regular file", path)
	}
	bridgeIndex, err := lastASCIIFoldIndexReaderAt(file, info.Size(), []byte("</body>"))
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("scan report page %s: %w", path, err)
	}
	writeErr := writeCompressedBundlePageReader(target, file, bridgeIndex)
	return errors.Join(writeErr, file.Close())
}

func writeCompressedBundlePageReader(target io.Writer, document io.Reader, bridgeIndex int64) error {
	encoded := base64.NewEncoder(base64.StdEncoding, target)
	compressed, err := gzip.NewWriterLevel(encoded, gzip.DefaultCompression)
	if err != nil {
		_ = encoded.Close()
		return err
	}

	if bridgeIndex >= 0 {
		_, err = io.CopyN(compressed, document, bridgeIndex)
		if err == nil {
			err = writeAll(compressed, []byte(bundledPageBridge))
		}
		if err == nil {
			_, err = io.Copy(compressed, document)
		}
	} else {
		_, err = io.Copy(compressed, document)
		if err == nil {
			err = writeAll(compressed, []byte(bundledPageBridge))
		}
	}
	return errors.Join(err, compressed.Close(), encoded.Close())
}

func lastASCIIFoldIndexReaderAt(source io.ReaderAt, size int64, target []byte) (int64, error) {
	if len(target) == 0 || size < int64(len(target)) {
		return -1, nil
	}
	const blockSize = 64 * 1024
	buffer := make([]byte, blockSize+len(target)-1)
	for end := size; end > 0; {
		start := max(int64(0), end-blockSize)
		readEnd := min(size, end+int64(len(target)-1))
		length := int(readEnd - start)
		read, err := source.ReadAt(buffer[:length], start)
		if err != nil && !errors.Is(err, io.EOF) {
			return -1, err
		}
		if read != length {
			return -1, io.ErrUnexpectedEOF
		}
		if index := lastASCIIFoldIndex(buffer[:length], target); index >= 0 {
			return start + int64(index), nil
		}
		end = start
	}
	return -1, nil
}

func lastASCIIFoldIndex(document, target []byte) int {
	if len(target) == 0 || len(document) < len(target) {
		return -1
	}
	for start := len(document) - len(target); start >= 0; start-- {
		matched := true
		for offset, expected := range target {
			actual := document[start+offset]
			if actual >= 'A' && actual <= 'Z' {
				actual += 'a' - 'A'
			}
			if expected >= 'A' && expected <= 'Z' {
				expected += 'a' - 'A'
			}
			if actual != expected {
				matched = false
				break
			}
		}
		if matched {
			return start
		}
	}
	return -1
}

// scriptSafeJSONWriter leaves ordinary HTML tags compact while escaping every closing tag slash.
// The escape is valid JSON and prevents an embedded </script> from terminating the raw-text payload.
// Holding a trailing '<' makes the transformation correct even when the underlying encoder splits
// the two-byte sequence across writes.
type scriptSafeJSONWriter struct {
	target      io.Writer
	pendingLess bool
}

func (w *scriptSafeJSONWriter) Write(data []byte) (int, error) {
	originalLength := len(data)
	if w.pendingLess && len(data) > 0 {
		prefix := []byte{'<'}
		if data[0] == '/' {
			prefix = []byte{'<', '\\'}
		}
		if err := writeAll(w.target, prefix); err != nil {
			return 0, err
		}
		w.pendingLess = false
	}
	start := 0
	for index, value := range data {
		if value != '<' {
			continue
		}
		if index+1 == len(data) {
			if err := writeAll(w.target, data[start:index]); err != nil {
				return 0, err
			}
			w.pendingLess = true
			return originalLength, nil
		}
		if data[index+1] != '/' {
			continue
		}
		if err := writeAll(w.target, data[start:index+1]); err != nil {
			return 0, err
		}
		if err := writeAll(w.target, []byte{'\\'}); err != nil {
			return 0, err
		}
		start = index + 1
	}
	if err := writeAll(w.target, data[start:]); err != nil {
		return 0, err
	}
	return originalLength, nil
}

func (w *scriptSafeJSONWriter) flush() error {
	if !w.pendingLess {
		return nil
	}
	w.pendingLess = false
	return writeAll(w.target, []byte{'<'})
}

func writeScriptSafeJSONString(target io.Writer, value string) error {
	writer := &scriptSafeJSONWriter{target: target}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return writer.flush()
}

func writeAll(target io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := target.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func encodeBundlePages(pages []BundlePage) ([]encodedBundlePage, error) {
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
		page.Path = strings.TrimSpace(page.Path)
		if page.ID == "" || page.Title == "" || page.Href == "" {
			return nil, fmt.Errorf("report bundle page %d has an empty id, title, or href", index)
		}
		if (len(page.HTML) == 0) == (page.Path == "") {
			return nil, fmt.Errorf("report bundle page %d must have exactly one document source", index)
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
			ID:      page.ID,
			Title:   page.Title,
			Href:    page.Href,
			Payload: fmt.Sprintf("jankhunter-report-page-payload-%d", index),
		})
	}
	return encoded, nil
}

const singleHTMLBundlePrefix = `<!doctype html>
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
    .report-brand::after { content: "JHLOG 2.0.0"; display: block; margin-top: 8px; color: rgba(16, 32, 23, .64); font-size: 11px; letter-spacing: .08em; }
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
<body data-jankhunter-single-html>
  <main class="report-shell">
    <header class="report-toolbar">
      <div class="report-brand"><img class="report-logo" src="` + reportLogoDataURI + `" alt="Jank Hunter"></div>
      <nav class="report-tabs" role="tablist" aria-label="Разделы отчета"></nav>
    </header>
    <section class="report-frames" aria-live="polite"></section>
  </main>
  <script id="jankhunter-report-pages" type="application/json">`

const singleHTMLBundleSuffix = `  <script>
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
      var frameLoadsByID = new Map();
      var selectedPageID = "";

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

      function decodeBase64(value) {
        var binary = atob(String(value || "").trim());
        var bytes = new Uint8Array(binary.length);
        for (var index = 0; index < binary.length; index += 1) {
          bytes[index] = binary.charCodeAt(index);
        }
        return bytes;
      }

      async function decodePagePayload(payload) {
        if (payload.dataset.encoding !== "gzip-base64") {
          throw new Error("unsupported page payload encoding");
        }
        if (typeof DecompressionStream !== "function") {
          throw new Error("gzip decompression is unavailable");
        }
        var compressed = decodeBase64(payload.textContent);
        var stream = new Blob([compressed]).stream().pipeThrough(new DecompressionStream("gzip"));
        return new Response(stream).text();
      }

      function ensureFrame(page) {
        var frame = framesByID.get(page.id);
        if (frame) return Promise.resolve(frame);
        var activeLoad = frameLoadsByID.get(page.id);
        if (activeLoad) return activeLoad;

        var load = (async function () {
          var html;
          var payload = document.getElementById(page.payload);
          try {
            if (!payload) throw new Error("missing page payload");
            html = await decodePagePayload(payload);
          } catch (error) {
            html = '<!doctype html><html lang="ru"><body><p>Не удалось прочитать выбранный раздел отчета.</p></body></html>';
          } finally {
            if (payload) payload.remove();
          }
          frame = document.createElement("iframe");
          frame.className = "report-frame";
          frame.id = "jankhunter-report-frame-" + page.id;
          frame.title = page.title;
          frame.dataset.page = page.id;
          frame.srcdoc = html;
          frames.appendChild(frame);
          framesByID.set(page.id, frame);
          frameLoadsByID.delete(page.id);
          return frame;
        })();
        frameLoadsByID.set(page.id, load);
        return load;
      }

      async function showPage(page, updateLocation) {
        if (!page) return;
        selectedPageID = page.id;
        framesByID.forEach(function (frame) { frame.classList.remove("active"); });
        buttonsByID.forEach(function (button) { button.setAttribute("aria-selected", "false"); });
        var button = buttonsByID.get(page.id);
        button.setAttribute("aria-selected", "true");
        button.scrollIntoView({block: "nearest", inline: "nearest"});
        var frame = await ensureFrame(page);
        if (selectedPageID !== page.id) return;
        frame.classList.add("active");
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
