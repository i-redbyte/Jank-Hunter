package report

const baseCSS = `
:root {
  color-scheme: light;
  --bg: #f6f7f9;
  --ink: #1e2329;
  --muted: #68717d;
  --panel: #ffffff;
  --line: #d9dee5;
  --accent: #c82032;
  --accent-2: #164b7a;
  --warn: #b76e00;
  --bad: #b42318;
  --ok: #0b7a46;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  font: 14px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  color: var(--ink);
  background: var(--bg);
}
header {
  padding: 28px 36px 22px;
  background: #111820;
  color: white;
}
h1 { margin: 0 0 8px; font-size: 28px; letter-spacing: 0; }
h2 { margin: 28px 0 12px; font-size: 18px; }
main { max-width: 1180px; margin: 0 auto; padding: 24px; }
.muted { color: var(--muted); }
header .muted { color: #b7c0ca; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
.metric, section {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: 8px;
}
.metric { padding: 14px 16px; }
.metric .label { color: var(--muted); font-size: 12px; text-transform: uppercase; }
.metric .value { font-size: 24px; font-weight: 700; margin-top: 4px; }
section { padding: 16px; margin: 16px 0; }
table { width: 100%; border-collapse: collapse; }
th, td { padding: 9px 10px; border-bottom: 1px solid var(--line); text-align: left; vertical-align: top; }
th { color: var(--muted); font-size: 12px; text-transform: uppercase; }
.bar { height: 8px; background: #e9edf2; border-radius: 99px; overflow: hidden; min-width: 90px; }
.bar > i { display: block; height: 100%; background: var(--accent); }
.sev-high { color: var(--bad); font-weight: 700; }
.sev-medium { color: var(--warn); font-weight: 700; }
.sev-ok { color: var(--ok); font-weight: 700; }
code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
`

const inspectTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter Inspect</title>
  <style>` + baseCSS + `</style>
</head>
<body>
<header>
  <h1>Jank Hunter Inspect</h1>
  <div class="muted">{{.Summary.Title}} · generated {{.GeneratedAt}}</div>
</header>
<main>
  <div class="grid">
    <div class="metric"><div class="label">Logs</div><div class="value">{{.Summary.LogCount}}</div></div>
    <div class="metric"><div class="label">Events</div><div class="value">{{.Summary.EventCount}}</div></div>
    <div class="metric"><div class="label">HTTP p95</div><div class="value">{{.Summary.HTTPP95MS}} ms</div></div>
    <div class="metric"><div class="label">UI jank</div><div class="value">{{printf "%.2f" .Summary.UIJankPct}}%</div></div>
    <div class="metric"><div class="label">Avg FPS</div><div class="value">{{printf "%.1f" .Summary.UIAvgFPS}}</div></div>
    <div class="metric"><div class="label">Max stall</div><div class="value">{{.Summary.StallMaxMS}} ms</div></div>
    <div class="metric"><div class="label">Max PSS</div><div class="value">{{.Summary.MemoryMaxKB}} KB</div></div>
  </div>

  <section>
    <h2>Slow Routes</h2>
    <table>
      <tr><th>Route</th><th>Count</th><th>Failures</th><th>p50</th><th>p95</th><th>Max</th><th>Avg TTFB</th><th>Owner</th></tr>
      {{range .Summary.Routes}}
      <tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P50MS}} ms</td><td>{{.P95MS}} ms</td><td>{{.MaxMS}} ms</td><td>{{.AvgTTFBMS}} ms</td><td><code>{{.OwnerSample}}</code></td></tr>
      {{else}}<tr><td colspan="8" class="muted">No HTTP events.</td></tr>{{end}}
    </table>
  </section>

  <section>
    <h2>Janky Screens</h2>
    <table>
      <tr><th>Screen</th><th>Frames</th><th>Janky</th><th>Jank rate</th><th>Avg FPS</th><th>Min FPS</th><th>p95 frame</th><th>max p99</th></tr>
      {{range .Summary.Screens}}
      <tr>
        <td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{.JankyFrames}}</td>
        <td><div>{{printf "%.2f" .JankRatePct}}%</div><div class="bar"><i style="{{pctWidth .JankRatePct}}"></i></div></td>
        <td>{{printf "%.1f" .AvgFPS}}</td><td>{{printf "%.1f" .MinFPS}}</td><td>{{.P95MS}} ms</td><td>{{.MaxP99MS}} ms</td>
      </tr>
      {{else}}<tr><td colspan="8" class="muted">No UI window events.</td></tr>{{end}}
    </table>
  </section>

  <section>
    <h2>Top Suspects</h2>
    <table>
      <tr><th>Owner / Class</th><th>Kind</th><th>Count</th><th>Total</th><th>Max</th><th>Stack hint</th></tr>
      {{range .Summary.Owners}}
      <tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.TotalMS}} ms</td><td>{{.MaxMS}} ms</td><td><code>{{.StackHint}}</code></td></tr>
      {{else}}<tr><td colspan="6" class="muted">No owner attribution yet.</td></tr>{{end}}
    </table>
  </section>

  <section>
    <h2>Counters</h2>
    <table>
      <tr><th>Name</th><th>Value</th></tr>
      {{range .Summary.Counters}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No counters.</td></tr>{{end}}
    </table>
  </section>

  <section>
    <h2>Gauges</h2>
    <table>
      <tr><th>Name</th><th>Average</th><th>Details</th></tr>
      {{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">No gauges.</td></tr>{{end}}
    </table>
  </section>
</main>
</body>
</html>`

const compareTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter Compare</title>
  <style>` + baseCSS + `</style>
</head>
<body>
<header>
  <h1>Jank Hunter Compare</h1>
  <div class="muted">generated {{.GeneratedAt}}</div>
</header>
<main>
  <section>
    <h2>Regression Summary</h2>
    <table>
      <tr><th>Metric</th><th>Baseline</th><th>Candidate</th><th>Change</th><th>Severity</th></tr>
      {{range .Comparison.Deltas}}
      <tr><td>{{.Name}}</td><td>{{.Baseline}}</td><td>{{.Candidate}}</td><td>{{.Change}}</td><td class="{{severityClass .Severity}}">{{.Severity}}</td></tr>
      {{end}}
    </table>
  </section>

  <div class="grid">
    <div class="metric"><div class="label">Baseline HTTP p95</div><div class="value">{{.Comparison.Baseline.HTTPP95MS}} ms</div></div>
    <div class="metric"><div class="label">Candidate HTTP p95</div><div class="value">{{.Comparison.Candidate.HTTPP95MS}} ms</div></div>
    <div class="metric"><div class="label">Baseline UI jank</div><div class="value">{{printf "%.2f" .Comparison.Baseline.UIJankPct}}%</div></div>
    <div class="metric"><div class="label">Candidate UI jank</div><div class="value">{{printf "%.2f" .Comparison.Candidate.UIJankPct}}%</div></div>
    <div class="metric"><div class="label">Baseline Avg FPS</div><div class="value">{{printf "%.1f" .Comparison.Baseline.UIAvgFPS}}</div></div>
    <div class="metric"><div class="label">Candidate Avg FPS</div><div class="value">{{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</div></div>
  </div>

  <section>
    <h2>Candidate Top Suspects</h2>
    <table>
      <tr><th>Owner / Class</th><th>Kind</th><th>Count</th><th>Total</th><th>Max</th><th>Stack hint</th></tr>
      {{range .Comparison.Candidate.Owners}}
      <tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.TotalMS}} ms</td><td>{{.MaxMS}} ms</td><td><code>{{.StackHint}}</code></td></tr>
      {{else}}<tr><td colspan="6" class="muted">No owner attribution yet.</td></tr>{{end}}
    </table>
  </section>
</main>
</body>
</html>`
