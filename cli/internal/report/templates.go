package report

const baseCSS = `
:root {
  color-scheme: dark;
  --bg: #070a12;
  --bg-2: #0b1020;
  --panel: rgba(12, 18, 34, 0.82);
  --panel-strong: rgba(17, 28, 52, 0.94);
  --line: rgba(126, 247, 255, 0.18);
  --line-strong: rgba(126, 247, 255, 0.36);
  --ink: #eef8ff;
  --muted: #90a7bb;
  --cyan: #6ff7ff;
  --blue: #6a8cff;
  --magenta: #ff4fd8;
  --warn: #ffd166;
  --bad: #ff5b7c;
  --ok: #62ffa8;
  --shadow: 0 20px 70px rgba(0, 0, 0, 0.38);
}
* { box-sizing: border-box; }
html { scroll-behavior: smooth; }
body {
  margin: 0;
  min-height: 100vh;
  font: 14px/1.5 Inter, "SF Pro Text", "Segoe UI", Roboto, Arial, sans-serif;
  color: var(--ink);
  background:
    radial-gradient(circle at 15% 0%, rgba(111, 247, 255, 0.18), transparent 28rem),
    radial-gradient(circle at 85% 10%, rgba(255, 79, 216, 0.16), transparent 30rem),
    linear-gradient(135deg, var(--bg), var(--bg-2) 52%, #070812);
}
body::before {
  content: "";
  position: fixed;
  inset: 0;
  pointer-events: none;
  background:
    linear-gradient(rgba(255,255,255,0.035) 1px, transparent 1px),
    linear-gradient(90deg, rgba(255,255,255,0.026) 1px, transparent 1px);
  background-size: 42px 42px;
  mask-image: linear-gradient(to bottom, rgba(0,0,0,0.92), transparent 82%);
}
body::after {
  content: "";
  position: fixed;
  left: 0;
  right: 0;
  top: -30vh;
  height: 24vh;
  pointer-events: none;
  background: linear-gradient(to bottom, transparent, rgba(111,247,255,0.09), transparent);
  animation: scan 12s linear infinite;
}
@keyframes scan {
  0% { transform: translateY(0); opacity: 0; }
  14% { opacity: 1; }
  70% { opacity: 0.35; }
  100% { transform: translateY(150vh); opacity: 0; }
}
@keyframes rise {
  from { transform: translateY(10px); opacity: 0; }
  to { transform: translateY(0); opacity: 1; }
}
@keyframes glow {
  0%, 100% { box-shadow: 0 0 0 rgba(111,247,255,0); }
  50% { box-shadow: 0 0 34px rgba(111,247,255,0.16); }
}
@keyframes mathGlow {
  0%, 100% { box-shadow: 0 0 0 rgba(98,255,168,0.05), 0 0 16px rgba(98,255,168,0.18); }
  50% { box-shadow: 0 0 0 4px rgba(98,255,168,0.08), 0 0 34px rgba(98,255,168,0.48); }
}
a { color: var(--cyan); text-decoration: none; }
a:hover { color: white; }
.hero {
  position: relative;
  padding: 34px 36px 24px;
  border-bottom: 1px solid var(--line);
  background: linear-gradient(120deg, rgba(10,16,30,0.92), rgba(23,14,44,0.72));
  box-shadow: var(--shadow);
}
.hero::after {
  content: "";
  position: absolute;
  inset: auto 36px 0;
  height: 1px;
  background: linear-gradient(90deg, transparent, var(--cyan), var(--magenta), transparent);
}
.hero-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 24px;
  align-items: start;
  max-width: 1280px;
  margin: 0 auto;
}
.eyebrow {
  color: var(--cyan);
  font-size: 12px;
  font-weight: 800;
  letter-spacing: 0.14em;
  text-transform: uppercase;
}
h1 {
  margin: 6px 0 10px;
  font-size: clamp(34px, 6vw, 68px);
  line-height: 0.92;
  letter-spacing: 0;
}
h2 { margin: 0 0 14px; font-size: 18px; letter-spacing: 0; }
h3 { margin: 18px 0 10px; font-size: 14px; color: var(--cyan); letter-spacing: 0.04em; text-transform: uppercase; }
.subhead { max-width: 880px; color: var(--muted); font-size: 15px; }
.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}
.math-link {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 38px;
  padding: 9px 16px;
  border: 1px solid rgba(98,255,168,0.74);
  border-radius: 999px;
  color: #03130a;
  background: linear-gradient(135deg, #62ffa8, #c9ff7a);
  font-weight: 950;
  letter-spacing: 0.02em;
  text-transform: none;
  animation: mathGlow 2.7s ease-in-out infinite;
}
.math-link:hover {
  color: #03130a;
  filter: brightness(1.08);
}
.hero-side { display: grid; gap: 10px; width: min(420px, 38vw); }
.hero-meta { display: grid; gap: 8px; }
.env-card {
  position: relative;
  overflow: hidden;
  border: 1px solid var(--line-strong);
  border-radius: 8px;
  padding: 14px;
  background:
    linear-gradient(135deg, rgba(111,247,255,0.10), rgba(255,79,216,0.07)),
    rgba(7,10,18,0.78);
  box-shadow: 0 18px 54px rgba(0,0,0,0.34), inset 0 0 0 1px rgba(255,255,255,0.04);
  animation: rise 520ms ease both, glow 5.4s ease-in-out infinite;
}
.env-card::before {
  content: "";
  position: absolute;
  left: 14px;
  right: 14px;
  top: 0;
  height: 1px;
  background: linear-gradient(90deg, transparent, var(--cyan), var(--magenta), transparent);
}
.env-title { font-size: 12px; color: var(--cyan); font-weight: 850; letter-spacing: 0.12em; text-transform: uppercase; }
.env-device { display: block; margin-top: 6px; font-size: 18px; line-height: 1.2; overflow-wrap: anywhere; }
.env-subtitle { margin-top: 2px; color: var(--muted); font-size: 12px; overflow-wrap: anywhere; }
.env-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; margin-top: 12px; }
.env-item {
  min-width: 0;
  padding: 9px;
  border: 1px solid rgba(126,247,255,0.13);
  border-radius: 8px;
  background: rgba(255,255,255,0.035);
}
.env-label { color: var(--muted); font-size: 10px; font-weight: 800; letter-spacing: 0.08em; text-transform: uppercase; }
.env-value { margin-top: 2px; font-weight: 850; overflow-wrap: anywhere; }
.env-detail { margin-top: 1px; color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }
.chip, .nav a {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 30px;
  padding: 6px 10px;
  border: 1px solid var(--line);
  border-radius: 999px;
  color: var(--muted);
  background: rgba(255,255,255,0.04);
}
.chip strong { color: var(--ink); margin-left: 6px; }
.nav {
  position: sticky;
  top: 0;
  z-index: 2;
  display: flex;
  gap: 8px;
  overflow-x: auto;
  padding: 12px 24px;
  border-bottom: 1px solid var(--line);
  background: rgba(7, 10, 18, 0.86);
  backdrop-filter: blur(18px);
}
.nav a { white-space: nowrap; }
main {
  position: relative;
  max-width: 1280px;
  margin: 0 auto;
  padding: 24px;
}
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 14px; }
.metric, .panel, .log-card {
  border: 1px solid var(--line);
  border-radius: 8px;
  background: linear-gradient(180deg, var(--panel-strong), var(--panel));
  box-shadow: var(--shadow);
}
.metric {
  position: relative;
  overflow: hidden;
  padding: 16px;
  animation: rise 420ms ease both;
}
.metric::before {
  content: "";
  position: absolute;
  inset: 0;
  border-top: 1px solid rgba(255,255,255,0.12);
  pointer-events: none;
}
.metric .label {
  color: var(--muted);
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.1em;
  text-transform: uppercase;
}
.metric .value {
  margin-top: 6px;
  font-size: 28px;
  font-weight: 850;
  letter-spacing: 0;
}
.metric .hint { margin-top: 4px; color: var(--muted); font-size: 12px; }
.panel, .log-card {
  margin: 18px 0;
  padding: 18px;
  animation: rise 520ms ease both;
}
.panel-head {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: start;
  margin-bottom: 14px;
}
.panel-kicker {
  color: var(--muted);
  font-size: 12px;
}
.split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.triad { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; }
.ring-row { display: flex; gap: 18px; flex-wrap: wrap; align-items: center; }
.gauge {
  --value: 0;
  --color: var(--cyan);
  width: 156px;
  aspect-ratio: 1;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background:
    radial-gradient(circle at center, rgba(8,12,24,0.96) 0 58%, transparent 59%),
    conic-gradient(var(--color) calc(var(--value) * 1%), rgba(255,255,255,0.08) 0);
  border: 1px solid var(--line);
  animation: glow 4.8s ease-in-out infinite;
}
.gauge-core {
  width: 112px;
  aspect-ratio: 1;
  border-radius: 50%;
  display: grid;
  place-items: center;
  text-align: center;
  background: rgba(7,10,18,0.88);
  border: 1px solid var(--line);
}
.gauge-core strong { display: block; font-size: 23px; line-height: 1; }
.gauge-core span { display: block; margin-top: 5px; color: var(--muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.08em; }
table { width: 100%; border-collapse: collapse; overflow: hidden; }
th, td { padding: 10px 11px; border-bottom: 1px solid rgba(126,247,255,0.12); text-align: left; vertical-align: top; }
th { color: var(--muted); font-size: 11px; letter-spacing: 0.08em; text-transform: uppercase; }
tr:hover td { background: rgba(111,247,255,0.035); }
.muted { color: var(--muted); }
code {
  color: #d8fcff;
  font-family: "JetBrains Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.bar, .chart-track, .delta-track {
  height: 10px;
  min-width: 90px;
  overflow: hidden;
  border-radius: 999px;
  background: rgba(255,255,255,0.08);
  border: 1px solid rgba(255,255,255,0.06);
}
.bar > i, .chart-track > i, .delta-track > i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, var(--cyan), var(--blue));
  box-shadow: 0 0 18px rgba(111,247,255,0.46);
}
.chart-list { display: grid; gap: 11px; }
.chart-row {
  display: grid;
  grid-template-columns: minmax(160px, 1.1fr) minmax(140px, 3fr) 94px;
  gap: 12px;
  align-items: center;
}
.warn > i, .sev-medium { color: var(--warn); }
.bad > i, .sev-high { color: var(--bad); }
.ok > i, .sev-ok { color: var(--ok); }
.delta-track > i.sev-high { background: linear-gradient(90deg, var(--bad), var(--magenta)); }
.delta-track > i.sev-medium { background: linear-gradient(90deg, var(--warn), var(--magenta)); }
.delta-track > i.sev-ok { background: linear-gradient(90deg, var(--ok), var(--cyan)); }
.sev-high, .sev-medium, .sev-ok { font-weight: 800; }
.warning {
  margin: 10px 0;
  padding: 12px 14px;
  border: 1px solid rgba(255,209,102,0.25);
  border-left: 4px solid var(--warn);
  border-radius: 8px;
  color: #ffe3a3;
  background: rgba(255,209,102,0.08);
}
.analysis-banner {
  display: grid;
  gap: 8px;
  padding: 16px;
  border: 1px solid var(--line-strong);
  border-radius: 8px;
  background: linear-gradient(120deg, rgba(111,247,255,0.10), rgba(255,79,216,0.08));
}
.analysis-banner.sev-high { border-color: rgba(255,91,124,0.55); }
.analysis-banner.sev-medium { border-color: rgba(255,209,102,0.55); }
.analysis-banner.sev-ok { border-color: rgba(98,255,168,0.45); }
.analysis-status {
  font-size: 24px;
  font-weight: 850;
}
.finding-list { display: grid; gap: 10px; margin-top: 14px; }
.finding {
  padding: 12px;
  border: 1px solid var(--line);
  border-left: 4px solid var(--cyan);
  border-radius: 8px;
  background: rgba(255,255,255,0.035);
}
.finding.sev-high { border-left-color: var(--bad); }
.finding.sev-medium { border-left-color: var(--warn); }
.finding.sev-ok { border-left-color: var(--ok); }
.finding strong { display: block; margin-bottom: 4px; }
.recommendations { margin: 10px 0 0; padding-left: 20px; color: var(--muted); }
.recommendations li { margin: 6px 0; }
.pill {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid var(--line);
  color: var(--muted);
  background: rgba(255,255,255,0.04);
  font-size: 12px;
}
.detail-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 14px; }
.fold {
  margin: 12px 0 0;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255,255,255,0.026);
  overflow: hidden;
}
.fold > summary {
  cursor: pointer;
  list-style: none;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  padding: 13px 14px;
  color: #d8fcff;
  font-weight: 800;
}
.fold > summary::-webkit-details-marker { display: none; }
.fold > summary::after {
  content: "open";
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.fold[open] > summary::after { content: "close"; }
.fold-body {
  max-height: 72vh;
  overflow: auto;
  padding: 0 14px 14px;
  border-top: 1px solid var(--line);
}
details.log-card { padding: 0; }
details.log-card summary {
  cursor: pointer;
  list-style: none;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  align-items: center;
  padding: 16px 18px;
}
details.log-card summary::-webkit-details-marker { display: none; }
.log-body { max-height: 72vh; overflow: auto; padding: 0 18px 18px; border-top: 1px solid var(--line); }
.summary-metrics { display: flex; flex-wrap: wrap; gap: 8px; justify-content: flex-end; }
.mono-block { overflow-wrap: anywhere; }
@media (max-width: 820px) {
  .hero { padding: 28px 18px 18px; }
  .hero-grid, .split, .triad, .detail-grid { grid-template-columns: 1fr; }
  .hero-side { width: 100%; }
  .env-grid { grid-template-columns: 1fr; }
  main { padding: 18px; }
  .panel-head { display: block; }
  .chart-row { grid-template-columns: 1fr; }
  details.log-card summary { grid-template-columns: 1fr; }
  .summary-metrics { justify-content: flex-start; }
}
`

const mathCSS = `
.math-page .fold > summary::after { content: "открыть"; }
.math-page .fold[open] > summary::after { content: "закрыть"; }
.math-page .nav {
  box-shadow: 0 14px 36px rgba(0,0,0,0.22);
}
.math-page .nav a[href="#method-reference"] {
  border-color: rgba(98,255,168,0.45);
  color: var(--ok);
}
.math-page .section-status {
  display: inline-flex;
  align-items: center;
  min-height: 26px;
  padding: 4px 9px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: rgba(255,255,255,0.04);
  font-size: 12px;
  font-weight: 850;
}
.math-page .section-status.sev-high { border-color: rgba(255,91,124,0.48); color: var(--bad); }
.math-page .section-status.sev-medium { border-color: rgba(255,209,102,0.48); color: var(--warn); }
.math-page .section-status.sev-ok { border-color: rgba(98,255,168,0.38); color: var(--ok); }
.math-page .source-list {
  display: grid;
  gap: 6px;
  max-height: 220px;
  overflow: auto;
}
.math-page .source-list code { display: block; }
.math-page .math-summary {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) minmax(280px, 0.8fr);
  gap: 16px;
}
.section-overview-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
  gap: 10px;
  margin-top: 12px;
}
.section-overview-card {
  display: grid;
  gap: 7px;
  min-width: 0;
  padding: 12px;
  border: 1px solid var(--line);
  border-left: 4px solid var(--cyan);
  border-radius: 8px;
  background: rgba(255,255,255,0.032);
}
.section-overview-card.sev-high { border-left-color: var(--bad); }
.section-overview-card.sev-medium { border-left-color: var(--warn); }
.section-overview-card.sev-ok { border-left-color: var(--ok); }
.section-overview-title {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  align-items: start;
  color: var(--ink);
  font-weight: 850;
}
.section-overview-summary {
  color: var(--muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.timeline-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 12px;
  margin: 10px 0 16px;
}
.timeline-chart {
  border: 1px solid var(--line);
  border-radius: 8px;
  padding: 12px;
  background: rgba(255,255,255,0.032);
}
.timeline-chart-head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: baseline;
  margin-bottom: 8px;
}
.timeline-chart-title { font-weight: 850; overflow-wrap: anywhere; }
.timeline-chart-value { color: var(--muted); font-size: 12px; white-space: nowrap; }
.sparkline {
  width: 100%;
  height: 86px;
  display: block;
  overflow: visible;
}
.spark-axis {
  stroke: rgba(255,255,255,0.12);
  stroke-width: 1;
}
.spark-bars rect {
  fill: rgba(98,255,168,0.22);
}
.spark-line {
  fill: none;
  stroke: var(--ok);
  stroke-width: 2.4;
  stroke-linecap: round;
  stroke-linejoin: round;
  filter: drop-shadow(0 0 7px rgba(98,255,168,0.45));
}
.timeline-table th, .timeline-table td { white-space: nowrap; }
.timeline-table {
  display: block;
  overflow-x: auto;
}
.method-reference-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
  gap: 12px;
}
.method-reference-card { margin: 0; }
.method-reference-card .fold-body { max-height: none; }
.method-reference-card summary span:first-child { overflow-wrap: anywhere; }
.method-kind { color: var(--ok); }
.reference-block {
  margin: 12px 0;
  padding: 11px;
  border: 1px solid rgba(126,247,255,0.13);
  border-radius: 8px;
  background: rgba(255,255,255,0.026);
}
.reference-block strong {
  display: block;
  margin-bottom: 5px;
  color: var(--cyan);
}
.reference-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 8px;
}
.reference-list code {
  display: inline-flex;
  padding: 3px 7px;
  border: 1px solid rgba(126,247,255,0.15);
  border-radius: 999px;
  background: rgba(255,255,255,0.04);
}
@media (max-width: 820px) {
  .math-page .math-summary { grid-template-columns: 1fr; }
  .section-overview-grid { grid-template-columns: 1fr; }
  .timeline-chart-head { display: block; }
  .timeline-chart-value { margin-top: 3px; white-space: normal; }
}
`

const mathInspectTemplate = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter: математический анализ</title>
  <style>` + baseCSS + mathCSS + `</style>
</head>
<body class="math-page">
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter · математика</div>
      <h1>Математический анализ</h1>
      <div class="subhead">{{.Math.Title}} · создан {{.GeneratedAt}} · автономный HTML</div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Состояние данных</div>
        <strong class="env-device">{{.Math.Summary.EventCount}} событий</strong>
        <div class="env-subtitle">{{.Math.Summary.LogCount}} логов · {{.Math.Summary.DurationMS}} ms · {{.Math.Summary.HTTPCount}} HTTP</div>
        <div class="env-grid">
          <div class="env-item"><div class="env-label">UI-кадры</div><div class="env-value">{{.Math.Summary.UIFrames}}</div><div class="env-detail">jank {{.Math.Summary.UIJank}}</div></div>
          <div class="env-item"><div class="env-label">Контекст</div><div class="env-value">{{.Math.Summary.ContextCount}}</div><div class="env-detail">сэмплы памяти/сети</div></div>
        </div>
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  {{range .Math.Sections}}<a href="#{{.ID}}">{{.Title}}</a>{{end}}
  <a href="#method-reference">Справка по методам</a>
</nav>
<main>
  <section class="panel">
    <div class="panel-head">
      <div>
        <h2>Обзор качества данных</h2>
        <div class="panel-kicker">Каркас математического отчета подключен к существующей inspect-сводке.</div>
      </div>
      <span class="pill">математический отчет</span>
    </div>
    <div class="math-summary">
      <div class="grid">
        <div class="metric"><div class="label">События</div><div class="value">{{.Math.Summary.EventCount}}</div><div class="hint">{{.Math.Summary.LogCount}} логов</div></div>
        <div class="metric"><div class="label">HTTP</div><div class="value">{{.Math.Summary.HTTPCount}}</div><div class="hint">{{.Math.Summary.HTTPFailed}} ошибок</div></div>
        <div class="metric"><div class="label">UI jank</div><div class="value">{{printf "%.2f" .Math.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Summary.UIJank}} / {{.Math.Summary.UIFrames}} кадров</div></div>
        <div class="metric"><div class="label">Память</div><div class="value">{{.Math.Summary.MemoryMaxKB}} KB</div><div class="hint">макс. PSS</div></div>
      </div>
      <div>
        <h3>Исходные логи</h3>
        <div class="source-list">{{range .Math.SourcePaths}}<code>{{.}}</code>{{else}}<span class="muted">Исходные логи не указаны.</span>{{end}}</div>
      </div>
    </div>
    <h3>Находки</h3>
    <div class="finding-list">
      {{range .Math.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
      {{else}}<div class="muted">Нет находок качества данных.</div>{{end}}
    </div>
    <h3>Сводка разделов</h3>
    <div class="section-overview-grid">
      {{range .Math.Sections}}
      <a class="section-overview-card {{severityClass .Status}}" href="#{{.ID}}">
        <div class="section-overview-title"><span>{{.Title}}</span><span class="section-status {{severityClass .Status}}">{{statusLabel .Status}}</span></div>
        <div class="section-overview-summary">{{.Summary}}</div>
      </a>
      {{end}}
    </div>
  </section>

  {{$math := .Math}}
  {{range .Math.Sections}}
  <section id="{{.ID}}" class="panel">
    <div class="panel-head">
      <div><h2>{{.Title}}</h2><div class="panel-kicker">{{.Summary}}</div></div>
      <span class="section-status {{severityClass .Status}}">{{statusLabel .Status}}</span>
    </div>
    <details class="fold">
      <summary>Детали раздела</summary>
      <div class="fold-body">
        <div class="finding-list">
          {{range .Findings}}
          <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
          {{else}}<div class="muted">Подробные находки появятся после реализации вычислений этого раздела.</div>{{end}}
        </div>
        {{if eq .ID "timeline"}}
        <h3>Ряды сигналов</h3>
        <div class="timeline-grid">
          {{range $math.Series}}
          <div class="timeline-chart">
            <div class="timeline-chart-head"><div class="timeline-chart-title">{{.Name}}</div><div class="timeline-chart-value">макс. {{printf "%.1f" (seriesMax .)}} {{.Unit}} · последн. {{printf "%.1f" (seriesLast .)}} {{.Unit}}</div></div>
            {{sparkline .}}
          </div>
          {{else}}<div class="muted">Нет ненулевых рядов для отображения.</div>{{end}}
        </div>
        <h3>Бакеты</h3>
        <table class="timeline-table">
          <tr><th>Время</th><th>HTTP</th><th>Ошибки</th><th>HTTP средн.</th><th>HTTP p95</th><th>DNS кол-во</th><th>DNS средн.</th><th>Connect кол-во</th><th>Connect средн.</th><th>TTFB средн.</th><th>UI кадры</th><th>Jank кадры</th><th>Доля jank</th><th>PSS</th><th>Свободная RAM</th><th>RX дельта</th><th>TX дельта</th></tr>
          {{range $math.Timeline}}
          <tr><td>{{bucketRange .}}</td><td>{{.HTTPCount}}</td><td>{{.HTTPFailed}}</td><td>{{.HTTPAvgDurationMS}} ms</td><td>{{.HTTPP95DurationMS}} ms</td><td>{{.DNSCount}}</td><td>{{.DNSDurationMS}} ms</td><td>{{.ConnectCount}}</td><td>{{.ConnectDurationMS}} ms</td><td>{{.TTFBMS}} ms</td><td>{{.UIFrames}}</td><td>{{.UIJankyFrames}}</td><td>{{printf "%.2f" (jankPct .UIJankyFrames .UIFrames)}}%</td><td>{{.MemoryPSSKB}} KB</td><td>{{.AvailableMemoryKB}} KB</td><td>{{.TrafficRxBytes}}</td><td>{{.TrafficTxBytes}}</td></tr>
          {{else}}<tr><td colspan="17" class="muted">Недостаточно данных для надежного анализа.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "robust"}}
        <h3>Распределения</h3>
        <table class="timeline-table">
          <tr><th>Слой</th><th>Имя</th><th>Метрика</th><th>N</th><th>Медиана</th><th>p90</th><th>p95</th><th>p99</th><th>MAD</th><th>Усеченное среднее</th><th>Мин.</th><th>Макс.</th><th>Интервал p95</th><th>Качество</th></tr>
          {{range $math.RobustStats}}
          <tr>
            <td>{{.Dimension}}</td><td><code>{{.Name}}</code></td><td>{{.Metric}}</td><td>{{.Count}}</td>
            <td>{{printf "%.1f" .Median}} {{.Unit}}</td><td>{{printf "%.1f" .P90}} {{.Unit}}</td><td>{{printf "%.1f" .P95}} {{.Unit}}</td><td>{{printf "%.1f" .P99}} {{.Unit}}</td>
            <td>{{printf "%.1f" .MAD}} {{.Unit}}</td><td>{{printf "%.1f" .TrimmedMean}} {{.Unit}}</td><td>{{printf "%.1f" .Min}} {{.Unit}}</td><td>{{printf "%.1f" .Max}} {{.Unit}}</td>
            <td>{{if .HasP95Confidence}}{{printf "%.1f" .P95ConfidenceLow}}..{{printf "%.1f" .P95ConfidenceHigh}} {{.Unit}}{{else}}мало данных{{end}}</td>
            <td><span class="section-status {{severityClass .SampleQualitySeverity}}">{{.SampleQuality}}</span><div class="muted">{{.SampleDetail}}</div></td>
          </tr>
          {{else}}<tr><td colspan="14" class="muted">Недостаточно данных для робастной статистики.</td></tr>{{end}}
        </table>
        <p class="muted">MAD — медиана абсолютных отклонений от медианы. Усеченное среднее считается после отсечения 10% нижнего и верхнего хвоста. Интервал p95 — детерминированный bootstrap-интервал; на очень больших сигналах считается по ограниченной детерминированной выборке.</p>
        {{end}}
        {{if eq .ID "change-points"}}
        <h3>Сдвиги сигналов</h3>
        <table class="timeline-table">
          <tr><th>Сигнал</th><th>Время</th><th>Направление</th><th>До</th><th>После</th><th>Δ</th><th>Δ%</th><th>MAD до/после</th><th>Оценка</th><th>Контекст</th><th>Рекомендация</th></tr>
          {{range $math.ChangePoints}}
          <tr>
            <td>{{.Signal}}</td><td>{{printf "%.1fs" (seconds .TimeMS)}}</td><td>{{.Direction}}</td>
            <td>{{printf "%.1f" .BeforeMedian}} {{.Unit}}</td><td>{{printf "%.1f" .AfterMedian}} {{.Unit}}</td><td>{{printf "%+.1f" .Delta}} {{.Unit}}</td><td>{{printf "%+.1f" .DeltaPct}}%</td>
            <td>{{printf "%.1f" .BeforeMAD}} / {{printf "%.1f" .AfterMAD}}</td>
            <td><div>{{printf "%.2f" .Score}}</div><div class="bar"><i style="{{scoreWidth .Score}}"></i></div></td>
            <td>{{if .NearbyScreen}}экран <code>{{.NearbyScreen}}</code><br>{{end}}{{if .NearbyRoute}}маршрут <code>{{.NearbyRoute}}</code><br>{{end}}{{if .NearbyOwner}}источник <code>{{.NearbyOwner}}</code><br>{{end}}{{if .NearbyNetwork}}сеть <code>{{.NearbyNetwork}}</code>{{end}}</td>
            <td>{{.Recommendation}}</td>
          </tr>
          {{else}}<tr><td colspan="11" class="muted">Сильных точек изменения не найдено.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "periodic"}}
        <h3>Автокорреляция и спектр</h3>
        <table class="timeline-table">
          <tr><th>Сигнал</th><th>N</th><th>Первый значимый лаг</th><th>Полураспад</th><th>Энтропия</th><th>Главные лаги</th><th>Спектральные пики</th><th>Вывод</th></tr>
          {{range $math.Periodic}}
          <tr>
            <td>{{.Signal}}</td><td>{{.SampleCount}}</td>
            <td>{{if .FirstSignificantLagMS}}{{printf "%.1fs" (seconds .FirstSignificantLagMS)}}{{else}}-{{end}}</td>
            <td>{{if .DecayHalfLifeMS}}{{printf "%.1fs" (seconds .DecayHalfLifeMS)}}{{else}}-{{end}}</td>
            <td>{{printf "%.2f" .SpectralEntropy}}</td>
            <td>{{range .TopLags}}<div>{{printf "%.1fs" (seconds .LagMS)}} · r={{printf "%.2f" .Correlation}}</div>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{range .Peaks}}<div>{{printf "%.1fs" (seconds .PeriodMS)}} · пик/фон {{printf "%.2f" .PeakToBackground}} · доверие {{printf "%.2f" .Confidence}}</div>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="8" class="muted">Недостаточно данных для периодического анализа.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "network-loops"}}
        <h3>Кандидаты циклов</h3>
        <table class="timeline-table">
          <tr><th>Маршрут</th><th>Источник</th><th>Период</th><th>Доверие</th><th>Выгорание</th><th>Окно</th><th>Паттерн</th><th>Вероятная причина</th><th>Путь</th></tr>
          {{range $math.NetworkLoops}}
          <tr>
            <td>{{if .Route}}<code>{{.Route}}</code>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{if .Owner}}<code>{{.Owner}}</code>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{printf "%.1fs" (seconds .PeriodMS)}}</td>
            <td>{{printf "%.2f" .Confidence}}</td>
            <td>{{printf "%.1f" .BurnScore}}</td>
            <td>{{printf "%.1fs" (seconds .FirstMS)}}..{{printf "%.1fs" (seconds .LastMS)}}</td>
            <td>{{motifText .Motif}}</td>
            <td>{{.ProbableCause}}</td>
            <td>{{if .Path.Nodes}}{{pathText .Path}}<div class="muted">стоимость {{printf "%.2f" .Path.Cost}}</div>{{else}}<span class="muted">нет</span>{{end}}</td>
          </tr>
          {{else}}<tr><td colspan="9" class="muted">Сетевые циклы с достаточным доверием не найдены.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "integral"}}
        <h3>Площади под сигналами</h3>
        <table class="timeline-table">
          <tr><th>Оценка</th><th>Значение</th><th>Формула</th><th>Объяснение</th><th>Вывод</th></tr>
          {{range $math.IntegralScores}}
          <tr>
            <td>{{.Title}}</td>
            <td><span class="section-status {{severityClass .Severity}}">{{printf "%.1f" .Value}} {{.Unit}}</span></td>
            <td><code>{{.Formula}}</code></td>
            <td>{{.Explanation}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="5" class="muted">Недостаточно данных для интегральных оценок.</td></tr>{{end}}
        </table>
        <p class="muted">Интегрирование прямоугольное: для каждого бакета берется значение сигнала и умножается на длительность бакета Δt. Чем дольше длится деградация, тем больше итоговая площадь.</p>
        {{end}}
        {{if eq .ID "markov"}}
        <h3>Сводка состояний</h3>
        <table class="timeline-table">
          <tr><th>Здоровые -> плохие</th><th>Восстановление</th><th>Ожидаемое восстановление</th><th>Липкие состояния</th></tr>
          <tr>
            <td>{{$math.Markov.HealthyToBadCount}}</td>
            <td>{{printf "%.1f" (percent01 $math.Markov.BadToHealthyProbability)}}%</td>
            <td>{{printf "%.1f" $math.Markov.ExpectedRecoveryWindows}} бакетов</td>
            <td>{{range $math.Markov.StickyStates}}<div>{{markovState .State}} · {{printf "%.1f" (percent01 .Probability)}}% · {{.Count}} переходов</div>{{else}}<span class="muted">нет</span>{{end}}</td>
          </tr>
        </table>
        <h3>Последовательность бакетов</h3>
        <table class="timeline-table">
          <tr><th>Время</th><th>Состояние</th><th>Причина</th><th>Контекст</th></tr>
          {{range $math.Markov.States}}
          <tr>
            <td>{{printf "%.1fs" (seconds .TimeMS)}}</td>
            <td>{{markovState .State}}</td>
            <td>{{.Reason}}</td>
            <td>{{if .Screen}}экран <code>{{.Screen}}</code><br>{{end}}{{if .Route}}маршрут <code>{{.Route}}</code><br>{{end}}{{if .Owner}}источник <code>{{.Owner}}</code><br>{{end}}{{if .Network}}сеть <code>{{.Network}}</code>{{end}}</td>
          </tr>
          {{else}}<tr><td colspan="4" class="muted">Недостаточно бакетов для Markov-модели.</td></tr>{{end}}
        </table>
        <h3>Матрица переходов</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Количество</th><th>Вероятность</th></tr>
          {{range $math.Markov.Transitions}}
          <tr><td>{{markovState .From}}</td><td>{{markovState .To}}</td><td>{{.Count}}</td><td>{{printf "%.1f" (percent01 .Probability)}}%</td></tr>
          {{else}}<tr><td colspan="4" class="muted">Переходы недоступны.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "graph"}}
        <h3>Кратчайшие объясняющие пути</h3>
        <table class="timeline-table">
          <tr><th>От</th><th>К</th><th>Путь</th><th>Стоимость</th><th>Доверие</th></tr>
          {{range $math.CausalGraph.Paths}}
          <tr><td>{{.From}}</td><td>{{.To}}</td><td>{{pathText .}}</td><td>{{printf "%.2f" .Cost}}</td><td>{{printf "%.2f" .Confidence}}</td></tr>
          {{else}}<tr><td colspan="5" class="muted">Кратчайшие пути от симптомов к источникам/маршрутам не найдены.</td></tr>{{end}}
        </table>
        <h3>Вклад источников</h3>
        <table class="timeline-table">
          <tr><th>Ранг</th><th>Источник</th><th>Оценка</th></tr>
          {{range $math.CausalGraph.OwnerScores}}
          <tr><td>{{.Rank}}</td><td><code>{{.Owner}}</code></td><td>{{printf "%.2f" .Score}}</td></tr>
          {{else}}<tr><td colspan="3" class="muted">Источники не выделены.</td></tr>{{end}}
        </table>
        <h3>Ребра графа</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Тип</th><th>Наблюдения</th><th>Вес</th><th>Доверие</th><th>Описание</th></tr>
          {{range $math.CausalGraph.Edges}}
          <tr><td>{{.FromLabel}}</td><td>{{.ToLabel}}</td><td>{{causalKind .Kind}}</td><td>{{.Count}}</td><td>{{printf "%.2f" .Weight}}</td><td>{{printf "%.2f" .Confidence}}</td><td>{{.Description}}</td></tr>
          {{else}}<tr><td colspan="7" class="muted">Ребра недоступны.</td></tr>{{end}}
        </table>
        <h3>Все кратчайшие пары Floyd-Warshall</h3>
        <table class="timeline-table">
          <tr><th>От</th><th>К</th><th>Путь</th><th>Стоимость</th></tr>
          {{range $math.CausalGraph.AllPairs}}
          <tr><td>{{.From}}</td><td>{{.To}}</td><td>{{pathText .}}</td><td>{{printf "%.2f" .Cost}}</td></tr>
          {{else}}<tr><td colspan="4" class="muted">Пути между всеми парами не рассчитаны: граф пустой или слишком большой для компактного HTML.</td></tr>{{end}}
        </table>
        {{end}}
      </div>
    </details>
  </section>
  {{end}}
  <section id="method-reference" class="panel">
    <div class="panel-head">
      <div>
        <h2>Справка по методам</h2>
        <div class="panel-kicker">Что именно измеряет каждый математический метод, как считается результат и где границы применимости.</div>
      </div>
      <span class="pill">объяснимость</span>
    </div>
    <div class="method-reference-grid">
      {{range .MethodReferences}}
      <details class="fold method-reference-card" id="method-{{.ID}}">
        <summary><span>{{.Title}}</span><span class="method-kind">{{.Kind}}</span></summary>
        <div class="fold-body">
          <div class="reference-block"><strong>Что измеряет</strong>{{.Measures}}</div>
          <div class="reference-block"><strong>Как считается</strong>{{.Calculation}}</div>
          <div class="reference-block"><strong>Как читать</strong>{{.Interpretation}}</div>
          <div class="reference-block"><strong>Ограничения</strong>{{.Limitations}}</div>
          <div class="reference-block"><strong>Поля в inspect</strong><div class="reference-list">{{range .InspectFields}}<code>{{.}}</code>{{end}}</div></div>
          <div class="reference-block"><strong>Поля в compare</strong><div class="reference-list">{{range .CompareFields}}<code>{{.}}</code>{{end}}</div></div>
        </div>
      </details>
      {{end}}
    </div>
  </section>
</main>
</body>
</html>`

const mathCompareTemplate = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter: математическое сравнение</title>
  <style>` + baseCSS + mathCSS + `</style>
</head>
<body class="math-page">
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter · математическое сравнение</div>
      <h1>Математический анализ сравнения</h1>
      <div class="subhead">{{.Math.Title}} · создан {{.GeneratedAt}} · автономный HTML</div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Сравнение</div>
        <strong class="env-device">{{.Math.Baseline.Summary.LogCount}} → {{.Math.Candidate.Summary.LogCount}} логов</strong>
        <div class="env-subtitle">база {{.Math.Baseline.Summary.EventCount}} событий · кандидат {{.Math.Candidate.Summary.EventCount}} событий</div>
        <div class="env-grid">
          <div class="env-item"><div class="env-label">HTTP базы</div><div class="env-value">{{.Math.Baseline.Summary.HTTPCount}}</div><div class="env-detail">p95 {{.Math.Baseline.Summary.HTTPP95MS}} ms</div></div>
          <div class="env-item"><div class="env-label">HTTP кандидата</div><div class="env-value">{{.Math.Candidate.Summary.HTTPCount}}</div><div class="env-detail">p95 {{.Math.Candidate.Summary.HTTPP95MS}} ms</div></div>
        </div>
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  {{range .Math.Sections}}<a href="#{{.ID}}">{{.Title}}</a>{{end}}
  <a href="#method-reference">Справка по методам</a>
</nav>
<main>
  <section class="panel">
    <div class="panel-head">
      <div>
        <h2>Обзор сравнения</h2>
        <div class="panel-kicker">Каркас математического отчета сравнения подключен к сводкам базы и кандидата.</div>
      </div>
      <span class="pill">математическое сравнение</span>
    </div>
    <div class="grid">
      <div class="metric"><div class="label">События базы</div><div class="value">{{.Math.Baseline.Summary.EventCount}}</div><div class="hint">{{.Math.Baseline.Summary.LogCount}} логов</div></div>
      <div class="metric"><div class="label">События кандидата</div><div class="value">{{.Math.Candidate.Summary.EventCount}}</div><div class="hint">{{.Math.Candidate.Summary.LogCount}} логов</div></div>
      <div class="metric"><div class="label">Jank базы</div><div class="value">{{printf "%.2f" .Math.Baseline.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Baseline.Summary.UIFrames}} кадров</div></div>
      <div class="metric"><div class="label">Jank кандидата</div><div class="value">{{printf "%.2f" .Math.Candidate.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Candidate.Summary.UIFrames}} кадров</div></div>
    </div>
    <h3>Находки</h3>
    <div class="finding-list">
      {{range .Math.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
      {{else}}<div class="muted">Нет находок качества сравнения.</div>{{end}}
    </div>
    <h3>Сводка разделов</h3>
    <div class="section-overview-grid">
      {{range .Math.Sections}}
      <a class="section-overview-card {{severityClass .Status}}" href="#{{.ID}}">
        <div class="section-overview-title"><span>{{.Title}}</span><span class="section-status {{severityClass .Status}}">{{statusLabel .Status}}</span></div>
        <div class="section-overview-summary">{{.Summary}}</div>
      </a>
      {{end}}
    </div>
  </section>

  {{$math := .Math}}
  {{range .Math.Sections}}
  <section id="{{.ID}}" class="panel">
    <div class="panel-head">
      <div><h2>{{.Title}}</h2><div class="panel-kicker">{{.Summary}}</div></div>
      <span class="section-status {{severityClass .Status}}">{{statusLabel .Status}}</span>
    </div>
    <details class="fold">
      <summary>Детали раздела</summary>
      <div class="fold-body">
        <div class="finding-list">
          {{range .Findings}}
          <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
          {{else}}<div class="muted">Подробные находки сравнения появятся после реализации вычислений этого раздела.</div>{{end}}
        </div>
        {{if eq .ID "timeline"}}
        <h3>База</h3>
        <div class="timeline-grid">
          {{range $math.Baseline.Series}}
          <div class="timeline-chart">
            <div class="timeline-chart-head"><div class="timeline-chart-title">{{.Name}}</div><div class="timeline-chart-value">макс. {{printf "%.1f" (seriesMax .)}} {{.Unit}}</div></div>
            {{sparkline .}}
          </div>
          {{else}}<div class="muted">Нет ненулевых рядов базы для отображения.</div>{{end}}
        </div>
        <h3>Кандидат</h3>
        <div class="timeline-grid">
          {{range $math.Candidate.Series}}
          <div class="timeline-chart">
            <div class="timeline-chart-head"><div class="timeline-chart-title">{{.Name}}</div><div class="timeline-chart-value">макс. {{printf "%.1f" (seriesMax .)}} {{.Unit}}</div></div>
            {{sparkline .}}
          </div>
          {{else}}<div class="muted">Нет ненулевых рядов кандидата для отображения.</div>{{end}}
        </div>
        {{end}}
        {{if eq .ID "robust"}}
        <h3>Робастные дельты</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Слой</th><th>Имя</th><th>Метрика</th><th>N база</th><th>N кандидат</th><th>p95 база</th><th>p95 кандидат</th><th>Δ p95</th><th>Δ%</th><th>Дельта Клиффа</th><th>Эффект</th><th>Доверие</th><th>Вывод</th></tr>
          {{range $math.RobustDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Dimension}}</td><td><code>{{.Name}}</code></td><td>{{.Metric}}</td><td>{{.BaselineCount}}</td><td>{{.CandidateCount}}</td>
            <td>{{printf "%.1f" .BaselineP95}} {{.Unit}}</td><td>{{printf "%.1f" .CandidateP95}} {{.Unit}}</td><td>{{printf "%+.1f" .P95Delta}} {{.Unit}}</td><td>{{printf "%+.1f" .P95DeltaPct}}%</td>
            <td>{{printf "%.3f" .CliffDelta}}</td><td>{{.EffectSize}}</td><td>{{.Confidence}}</td><td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="14" class="muted">Недостаточно пересекающихся распределений для робастного сравнения.</td></tr>{{end}}
        </table>
        <p class="muted">Положительная дельта Клиффа означает, что значения кандидата чаще больше базы. Для задержек, jank, памяти и очередей это обычно ухудшение.</p>
        {{end}}
        {{if eq .ID "change-points"}}
        <h3>Изменения точек изменения</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Сигнал</th><th>Время базы</th><th>Время кандидата</th><th>Оценка базы</th><th>Оценка кандидата</th><th>Вывод</th></tr>
          {{range $math.ChangeDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{.Status}}</span></td><td>{{.Signal}}</td>
            <td>{{if .BaselineTime}}{{printf "%.1fs" (seconds .BaselineTime)}}{{else}}-{{end}}</td>
            <td>{{if .CandidateTime}}{{printf "%.1fs" (seconds .CandidateTime)}}{{else}}-{{end}}</td>
            <td>{{printf "%.2f" .BaselineScore}}</td><td><div>{{printf "%.2f" .CandidateScore}}</div><div class="bar"><i style="{{scoreWidth .CandidateScore}}"></i></div></td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="7" class="muted">Новых, исчезнувших или усилившихся точек изменения не найдено.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "periodic"}}
        <h3>База</h3>
        <table class="timeline-table">
          <tr><th>Сигнал</th><th>N</th><th>Первый лаг</th><th>Энтропия</th><th>Пики</th><th>Вывод</th></tr>
          {{range $math.Baseline.Periodic}}
          <tr><td>{{.Signal}}</td><td>{{.SampleCount}}</td><td>{{if .FirstSignificantLagMS}}{{printf "%.1fs" (seconds .FirstSignificantLagMS)}}{{else}}-{{end}}</td><td>{{printf "%.2f" .SpectralEntropy}}</td><td>{{range .Peaks}}<div>{{printf "%.1fs" (seconds .PeriodMS)}} · доверие {{printf "%.2f" .Confidence}}</div>{{else}}<span class="muted">нет</span>{{end}}</td><td>{{.Summary}}</td></tr>
          {{else}}<tr><td colspan="6" class="muted">Недостаточно данных базы.</td></tr>{{end}}
        </table>
        <h3>Кандидат</h3>
        <table class="timeline-table">
          <tr><th>Сигнал</th><th>N</th><th>Первый лаг</th><th>Энтропия</th><th>Пики</th><th>Вывод</th></tr>
          {{range $math.Candidate.Periodic}}
          <tr><td>{{.Signal}}</td><td>{{.SampleCount}}</td><td>{{if .FirstSignificantLagMS}}{{printf "%.1fs" (seconds .FirstSignificantLagMS)}}{{else}}-{{end}}</td><td>{{printf "%.2f" .SpectralEntropy}}</td><td>{{range .Peaks}}<div>{{printf "%.1fs" (seconds .PeriodMS)}} · доверие {{printf "%.2f" .Confidence}}</div>{{else}}<span class="muted">нет</span>{{end}}</td><td>{{.Summary}}</td></tr>
          {{else}}<tr><td colspan="6" class="muted">Недостаточно данных кандидата.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "network-loops"}}
        <h3>Дельты сетевых циклов</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Маршрут</th><th>Источник</th><th>Период базы</th><th>Период кандидата</th><th>Выгорание базы</th><th>Выгорание кандидата</th><th>Δ выгорания</th><th>Δ доверия</th><th>Вывод</th></tr>
          {{range $math.NetworkLoopDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{.Status}}</span></td>
            <td>{{if .Route}}<code>{{.Route}}</code>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{if .Owner}}<code>{{.Owner}}</code>{{else}}<span class="muted">нет</span>{{end}}</td>
            <td>{{if .BaselinePeriodMS}}{{printf "%.1fs" (seconds .BaselinePeriodMS)}}{{else}}-{{end}}</td>
            <td>{{if .CandidatePeriodMS}}{{printf "%.1fs" (seconds .CandidatePeriodMS)}}{{else}}-{{end}}</td>
            <td>{{printf "%.1f" .BaselineBurn}}</td>
            <td>{{printf "%.1f" .CandidateBurn}}</td>
            <td>{{printf "%+.1f" .BurnDelta}}</td>
            <td>{{printf "%+.2f" .ConfidenceDelta}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="10" class="muted">Заметных изменений сетевых циклов не найдено.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "integral"}}
        <h3>Дельты интегральных оценок</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Оценка</th><th>База</th><th>Кандидат</th><th>Δ</th><th>Δ%</th><th>Формула</th><th>Вывод</th></tr>
          {{range $math.IntegralDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Title}}</td>
            <td>{{printf "%.1f" .BaselineValue}} {{.Unit}}</td>
            <td>{{printf "%.1f" .CandidateValue}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .Delta}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .DeltaPct}}%</td>
            <td><code>{{.Formula}}</code></td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="8" class="muted">Интегральные дельты недоступны.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "markov"}}
        <h3>Дельты Markov-метрик</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Метрика</th><th>База</th><th>Кандидат</th><th>Δ</th><th>Вывод</th></tr>
          {{range $math.MarkovDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Metric}}</td>
            <td>{{printf "%.1f" .BaselineValue}} {{.Unit}}</td>
            <td>{{printf "%.1f" .CandidateValue}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .Delta}} {{.Unit}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="6" class="muted">Markov-дельты недоступны.</td></tr>{{end}}
        </table>
        <h3>Переходы кандидата</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Количество</th><th>Вероятность</th></tr>
          {{range $math.Candidate.Markov.Transitions}}
          <tr><td>{{markovState .From}}</td><td>{{markovState .To}}</td><td>{{.Count}}</td><td>{{printf "%.1f" (percent01 .Probability)}}%</td></tr>
          {{else}}<tr><td colspan="4" class="muted">Переходы кандидата недоступны.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "graph"}}
        <h3>Дельты графа причинности</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Тип</th><th>База</th><th>Кандидат</th><th>Δ</th><th>Вывод</th></tr>
          {{range $math.CausalDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Kind}}</td>
            <td>{{printf "%.2f" .BaselineValue}}</td>
            <td>{{printf "%.2f" .CandidateValue}}</td>
            <td>{{printf "%+.2f" .Delta}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="6" class="muted">Заметных изменений графа не найдено.</td></tr>{{end}}
        </table>
        <h3>Кратчайшие пути кандидата</h3>
        <table class="timeline-table">
          <tr><th>От</th><th>К</th><th>Путь</th><th>Стоимость</th><th>Доверие</th></tr>
          {{range $math.Candidate.CausalGraph.Paths}}
          <tr><td>{{.From}}</td><td>{{.To}}</td><td>{{pathText .}}</td><td>{{printf "%.2f" .Cost}}</td><td>{{printf "%.2f" .Confidence}}</td></tr>
          {{else}}<tr><td colspan="5" class="muted">Кратчайшие пути кандидата не найдены.</td></tr>{{end}}
        </table>
        {{end}}
      </div>
    </details>
  </section>
  {{end}}
  <section id="method-reference" class="panel">
    <div class="panel-head">
      <div>
        <h2>Справка по методам</h2>
        <div class="panel-kicker">Что именно измеряет каждый математический метод, как считается результат и как читать дельты между базой и кандидатом.</div>
      </div>
      <span class="pill">объяснимость</span>
    </div>
    <div class="method-reference-grid">
      {{range .MethodReferences}}
      <details class="fold method-reference-card" id="method-{{.ID}}">
        <summary><span>{{.Title}}</span><span class="method-kind">{{.Kind}}</span></summary>
        <div class="fold-body">
          <div class="reference-block"><strong>Что измеряет</strong>{{.Measures}}</div>
          <div class="reference-block"><strong>Как считается</strong>{{.Calculation}}</div>
          <div class="reference-block"><strong>Как читать</strong>{{.Interpretation}}</div>
          <div class="reference-block"><strong>Ограничения</strong>{{.Limitations}}</div>
          <div class="reference-block"><strong>Поля в inspect</strong><div class="reference-list">{{range .InspectFields}}<code>{{.}}</code>{{end}}</div></div>
          <div class="reference-block"><strong>Поля в compare</strong><div class="reference-list">{{range .CompareFields}}<code>{{.}}</code>{{end}}</div></div>
        </div>
      </details>
      {{end}}
    </div>
  </section>
</main>
</body>
</html>`

const inspectTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter Inspect</title>
  <style>` + baseCSS + `</style>
</head>
<body>
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter Inspect</div>
      <h1>Runtime Signal Report</h1>
      <div class="subhead">{{.Summary.Title}} · generated {{.GeneratedAt}} · standalone offline HTML</div>
      <div class="hero-actions"><a class="math-link" href="{{.MathReportHref}}">λ Анализ</a></div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Device Context</div>
        <strong class="env-device">{{fallback .Summary.Environment.Title "unknown device"}}</strong>
        <div class="env-subtitle">{{fallback .Summary.Environment.Subtitle "runtime context unavailable"}}</div>
        <div class="env-grid">
          {{range .Summary.Environment.Items}}
          <div class="env-item"><div class="env-label">{{.Label}}</div><div class="env-value">{{.Value}}</div><div class="env-detail">{{.Detail}}</div></div>
          {{else}}<div class="env-item"><div class="env-label">Context</div><div class="env-value">unknown</div><div class="env-detail">No session/context metadata.</div></div>{{end}}
        </div>
      </div>
      <div class="hero-meta">
        <div class="chip">Logs <strong>{{.Summary.LogCount}}</strong></div>
        <div class="chip">Events <strong>{{.Summary.EventCount}}</strong></div>
        <div class="chip">Duration <strong>{{.Summary.DurationMS}} ms</strong></div>
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#overview">Overview</a>
  <a href="#network">Network</a>
  <a href="#ui">UI</a>
  <a href="#owners">Owners</a>
  <a href="#memory">Memory</a>
  <a href="#custom">Metrics</a>
  <a href="#context">Context</a>
  <a href="#analysis">Verdict</a>
</nav>
<main>
  <section id="overview" class="panel">
    <div class="panel-head">
      <div>
        <h2>Executive Signal Matrix</h2>
        <div class="panel-kicker">Fast read of the run: latency, smoothness, stalls, memory and traffic.</div>
      </div>
      <span class="pill">offline report</span>
    </div>
    <div class="grid">
      <div class="metric"><div class="label">HTTP p95</div><div class="value">{{.Summary.HTTPP95MS}} ms</div><div class="hint">{{.Summary.HTTPCount}} requests, {{.Summary.HTTPFailed}} failed</div></div>
      <div class="metric"><div class="label">UI jank</div><div class="value">{{printf "%.2f" .Summary.UIJankPct}}%</div><div class="hint">{{.Summary.UIJank}} / {{.Summary.UIFrames}} frames</div></div>
      <div class="metric"><div class="label">Average FPS</div><div class="value">{{printf "%.1f" .Summary.UIAvgFPS}}</div><div class="hint">min {{printf "%.1f" .Summary.UIMinFPS}}</div></div>
      <div class="metric"><div class="label">Max stall</div><div class="value">{{.Summary.StallMaxMS}} ms</div><div class="hint">{{.Summary.StallCount}} stall events</div></div>
      <div class="metric"><div class="label">Max PSS</div><div class="value">{{.Summary.MemoryMaxKB}} KB</div><div class="hint">retained {{.Summary.Retained}}</div></div>
      <div class="metric"><div class="label">UID RX max</div><div class="value">{{.Summary.TrafficRxMax}}</div><div class="hint">TX max {{.Summary.TrafficTxMax}}</div></div>
    </div>
    <h3>Health Gauges</h3>
    <div class="ring-row">
      <div class="gauge" style="{{ringStyle .Summary.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIJankPct}}%</strong><span>UI jank</span></div></div></div>
      <div class="gauge" style="{{ringStyle (rate .Summary.HTTPFailed .Summary.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Summary.HTTPFailed .Summary.HTTPCount)}}%</strong><span>HTTP fail</span></div></div></div>
      <div class="gauge" style="{{ringStyle (fpsScore .Summary.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIAvgFPS}}</strong><span>avg FPS</span></div></div></div>
    </div>
  </section>

  <section id="network" class="panel">
    <div class="panel-head">
      <div><h2>Network Routes</h2><div class="panel-kicker">Slowest routes by p95 latency, failures, bytes and owner attribution.</div></div>
    </div>
    <details class="fold">
      <summary>Route Details</summary>
      <div class="fold-body">
        <div class="chart-list">
          {{range .Summary.Routes}}
          <div class="chart-row"><code>{{.Route}}</code><div class="chart-track"><i style="{{msWidth .P95MS}}"></i></div><strong>{{.P95MS}} ms</strong></div>
          {{else}}<div class="muted">No HTTP events.</div>{{end}}
        </div>
        <h3>Route Table</h3>
        <table>
          <tr><th>Route</th><th>Count</th><th>Failures</th><th>p50</th><th>p95</th><th>Max</th><th>Avg TTFB</th><th>RX</th><th>TX</th><th>Owner</th></tr>
          {{range .Summary.Routes}}
          <tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P50MS}} ms</td><td>{{.P95MS}} ms</td><td>{{.MaxMS}} ms</td><td>{{.AvgTTFBMS}} ms</td><td>{{.BytesRx}}</td><td>{{.BytesTx}}</td><td><code>{{.OwnerSample}}</code></td></tr>
          {{else}}<tr><td colspan="10" class="muted">No HTTP events.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="ui" class="panel">
    <div class="panel-head">
      <div><h2>UI Smoothness</h2><div class="panel-kicker">Screens ranked by jank rate and frame latency.</div></div>
    </div>
    <details class="fold">
      <summary>Screen Details</summary>
      <div class="fold-body">
        <div class="chart-list">
          {{range .Summary.Screens}}
          <div class="chart-row"><code>{{.Screen}}</code><div class="chart-track warn"><i style="{{pctWidth .JankRatePct}}"></i></div><strong>{{printf "%.2f" .JankRatePct}}%</strong></div>
          {{else}}<div class="muted">No UI window events.</div>{{end}}
        </div>
        <h3>Screen Table</h3>
        <table>
          <tr><th>Screen</th><th>Windows</th><th>Frames</th><th>Janky</th><th>Jank rate</th><th>Avg FPS</th><th>Min FPS</th><th>p95 frame</th><th>max p99</th></tr>
          {{range .Summary.Screens}}
          <tr>
            <td><code>{{.Screen}}</code></td><td>{{.WindowCount}}</td><td>{{.Frames}}</td><td>{{.JankyFrames}}</td>
            <td><div>{{printf "%.2f" .JankRatePct}}%</div><div class="bar"><i style="{{pctWidth .JankRatePct}}"></i></div></td>
            <td>{{printf "%.1f" .AvgFPS}}</td><td>{{printf "%.1f" .MinFPS}}</td><td>{{.P95MS}} ms</td><td>{{.MaxP99MS}} ms</td>
          </tr>
          {{else}}<tr><td colspan="9" class="muted">No UI window events.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="owners" class="panel">
    <div class="panel-head">
      <div><h2>Attribution Hotspots</h2><div class="panel-kicker">Owners, classes and stack hints with the largest measured impact.</div></div>
    </div>
    <details class="fold">
      <summary>Owner Details</summary>
      <div class="fold-body">
        <table>
          <tr><th>Owner / Class</th><th>Kind</th><th>Count</th><th>Total</th><th>Max</th><th>Stack hint</th></tr>
          {{range .Summary.Owners}}
          <tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.TotalMS}} ms</td><td>{{.MaxMS}} ms</td><td><code>{{.StackHint}}</code></td></tr>
          {{else}}<tr><td colspan="6" class="muted">No owner attribution yet.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="memory" class="panel">
    <div class="panel-head">
      <div><h2>Memory And Retention</h2><div class="panel-kicker">PSS, available memory, low-memory samples and retained object age buckets.</div></div>
    </div>
    <details class="fold">
      <summary>Memory Details</summary>
      <div class="fold-body">
        <div class="split">
          <div>
            <h3>Memory</h3>
            <table>
              <tr><th>Metric</th><th>Value</th><th>Details</th></tr>
              {{range .Summary.Memory}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">No memory events.</td></tr>{{end}}
            </table>
          </div>
          <div>
            <h3>Retained Classes</h3>
            <table>
              <tr><th>Class / Owner</th><th>Count</th><th>Details</th></tr>
              {{range .Summary.RetainedClasses}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">No retained-object events.</td></tr>{{end}}
            </table>
          </div>
        </div>
        <h3>Retained Age Buckets</h3>
        <table>
          <tr><th>Age</th><th>Count</th></tr>
          {{range .Summary.RetainedAgeBuckets}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No retained-object events.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="custom" class="panel">
    <div class="panel-head">
      <div><h2>Custom Metrics</h2><div class="panel-kicker">Counters, gauges and AndroidX JankStats bridge metrics when available.</div></div>
    </div>
    <details class="fold">
      <summary>Metric Details</summary>
      <div class="fold-body">
        <div class="triad">
          <div>
            <h3>Counters</h3>
            <table><tr><th>Name</th><th>Value</th></tr>{{range .Summary.Counters}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No counters.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>Gauges</h3>
            <table><tr><th>Name</th><th>Average</th><th>Details</th></tr>{{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">No gauges.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>JankStats</h3>
            <table><tr><th>Metric</th><th>Value</th><th>Details</th></tr>{{range .Summary.JankStats}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">No JankStats metrics.</td></tr>{{end}}</table>
          </div>
        </div>
      </div>
    </details>
  </section>

  <section id="context" class="panel">
    <div class="panel-head">
      <div><h2>Run Context</h2><div class="panel-kicker">Cohorts keep comparisons honest: app, build, SDK, device, process and network.</div></div>
    </div>
    <details class="fold">
      <summary>Context Details</summary>
      <div class="fold-body">
        <div class="triad">
          <div><h3>App Versions</h3><table>{{range .Summary.AppVersions}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">unknown</td><td>0</td></tr>{{end}}</table></div>
          <div><h3>SDKs</h3><table>{{range .Summary.SDKs}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">unknown</td><td>0</td></tr>{{end}}</table></div>
          <div><h3>Devices</h3><table>{{range .Summary.Devices}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">unknown</td><td>0</td></tr>{{end}}</table></div>
        </div>
        <h3>Process Breakdown</h3>
        <table><tr><th>Process</th><th>Sessions</th></tr>{{range .Summary.Processes}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No process metadata.</td></tr>{{end}}</table>
        <h3>Network Samples</h3>
        <table><tr><th>Network</th><th>Samples</th></tr>{{range .Summary.Network}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No context events.</td></tr>{{end}}</table>
        <h3>Combined Cohorts</h3>
        <table><tr><th>Cohort</th><th>Events</th></tr>{{range .Summary.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No cohort metadata.</td></tr>{{end}}</table>
      </div>
    </details>
  </section>

  <section id="analysis" class="panel">
    <div class="panel-head">
      <div><h2>Heuristic Verdict</h2><div class="panel-kicker">Rule-based triage over all collected signals. Treat it as a review checklist, not as a mathematical proof.</div></div>
    </div>
    <div class="analysis-banner {{severityClass .Analysis.Severity}}">
      <div class="eyebrow">Overall status</div>
      <div class="analysis-status">{{.Analysis.Status}}</div>
      <div class="muted">{{.Analysis.Summary}}</div>
    </div>
    <h3>Findings</h3>
    <div class="finding-list">
      {{range .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">No heuristic findings.</div>{{end}}
    </div>
    <h3>Recommendations</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>No extra recommendations.</li>{{end}}
    </ul>
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
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter Compare</div>
      <h1>Regression Control Deck</h1>
      <div class="subhead">generated {{.GeneratedAt}} · compare first, then drill into every baseline and candidate log</div>
      <div class="hero-actions"><a class="math-link" href="{{.MathReportHref}}">λ Анализ</a></div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Candidate Device Context</div>
        <strong class="env-device">{{fallback .Comparison.Candidate.Environment.Title "unknown device"}}</strong>
        <div class="env-subtitle">{{fallback .Comparison.Candidate.Environment.Subtitle "runtime context unavailable"}}</div>
        <div class="env-grid">
          {{range .Comparison.Candidate.Environment.Items}}
          <div class="env-item"><div class="env-label">{{.Label}}</div><div class="env-value">{{.Value}}</div><div class="env-detail">{{.Detail}}</div></div>
          {{else}}<div class="env-item"><div class="env-label">Context</div><div class="env-value">unknown</div><div class="env-detail">No session/context metadata.</div></div>{{end}}
        </div>
      </div>
      <div class="hero-meta">
        <div class="chip">Baseline logs <strong>{{.Comparison.Baseline.LogCount}}</strong></div>
        <div class="chip">Candidate logs <strong>{{.Comparison.Candidate.LogCount}}</strong></div>
        <div class="chip">Deltas <strong>{{len .Comparison.Deltas}}</strong></div>
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#compare">Comparison</a>
  <a href="#regressions">Regressions</a>
  <a href="#candidate">Candidate Detail</a>
  <a href="#drilldown">Per-log Drill-down</a>
  <a href="#cohorts">Cohorts</a>
  <a href="#analysis">Verdict</a>
</nav>
<main>
  <section id="compare" class="panel">
    <div class="panel-head">
      <div>
        <h2>Comparative Scoreboard</h2>
        <div class="panel-kicker">Baseline vs candidate across latency, smoothness, memory, traffic, retention and cohort mix.</div>
      </div>
      <span class="pill">standalone HTML</span>
    </div>
    {{range .Comparison.Warnings}}<p class="warning">{{.}}</p>{{end}}
    <div class="grid">
      <div class="metric"><div class="label">Baseline HTTP p95</div><div class="value">{{.Comparison.Baseline.HTTPP95MS}} ms</div><div class="hint">{{.Comparison.Baseline.HTTPCount}} requests</div></div>
      <div class="metric"><div class="label">Candidate HTTP p95</div><div class="value">{{.Comparison.Candidate.HTTPP95MS}} ms</div><div class="hint">{{.Comparison.Candidate.HTTPCount}} requests</div></div>
      <div class="metric"><div class="label">Baseline UI jank</div><div class="value">{{printf "%.2f" .Comparison.Baseline.UIJankPct}}%</div><div class="hint">{{printf "%.1f" .Comparison.Baseline.UIAvgFPS}} avg FPS</div></div>
      <div class="metric"><div class="label">Candidate UI jank</div><div class="value">{{printf "%.2f" .Comparison.Candidate.UIJankPct}}%</div><div class="hint">{{printf "%.1f" .Comparison.Candidate.UIAvgFPS}} avg FPS</div></div>
      <div class="metric"><div class="label">Baseline Max PSS</div><div class="value">{{.Comparison.Baseline.MemoryMaxKB}} KB</div><div class="hint">retained {{.Comparison.Baseline.Retained}}</div></div>
      <div class="metric"><div class="label">Candidate Max PSS</div><div class="value">{{.Comparison.Candidate.MemoryMaxKB}} KB</div><div class="hint">retained {{.Comparison.Candidate.Retained}}</div></div>
    </div>
    <h3>Signal Rings</h3>
    <div class="ring-row">
      <div class="gauge" style="{{ringStyle .Comparison.Candidate.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIJankPct}}%</strong><span>candidate jank</span></div></div></div>
      <div class="gauge" style="{{ringStyle (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}%</strong><span>candidate fail</span></div></div></div>
      <div class="gauge" style="{{ringStyle (fpsScore .Comparison.Candidate.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</strong><span>candidate FPS</span></div></div></div>
    </div>
  </section>

  <section id="regressions" class="panel">
    <div class="panel-head">
      <div><h2>Regression Matrix</h2><div class="panel-kicker">Severity is adjusted for confidence and sample size. Bars show regression magnitude capped at 100%.</div></div>
    </div>
    <table>
      <tr><th>Metric</th><th>Baseline</th><th>Candidate</th><th>Change</th><th>Regression</th><th>Severity</th><th>Confidence</th><th>Sample</th><th>Interval</th></tr>
      {{range .Comparison.Deltas}}
      <tr>
        <td>{{.Name}}</td><td>{{.Baseline}}</td><td>{{.Candidate}}</td><td>{{.Change}}</td>
        <td><div class="delta-track"><i class="{{severityClass .Severity}}" style="{{deltaWidth .RegressionPct}}"></i></div></td>
        <td class="{{severityClass .Severity}}">{{.Severity}}</td><td>{{.Confidence}}</td><td>{{.SampleSize}}</td><td>{{.Interval}}</td>
      </tr>
      {{end}}
    </table>
    <h3>Worst Regression Cards</h3>
    <table>
      <tr><th>Metric</th><th>Severity</th><th>Regression</th><th>Confidence</th><th>Sample</th></tr>
      {{range .Comparison.Deltas}}
      {{if notOK .Severity}}<tr><td>{{.Name}}</td><td class="{{severityClass .Severity}}">{{.Severity}}</td><td>{{.Change}}</td><td>{{.Confidence}}</td><td>{{.SampleSize}}</td></tr>{{end}}
      {{end}}
    </table>
  </section>

  <section id="candidate" class="panel">
    <div class="panel-head">
      <div><h2>Candidate Deep Summary</h2><div class="panel-kicker">The aggregate candidate profile after all filters.</div></div>
    </div>
    <details class="fold">
      <summary>Candidate Route, Screen And Owner Details</summary>
      <div class="fold-body">
        <div class="split">
          <div>
            <h3>Candidate Routes</h3>
            <table>
              <tr><th>Route</th><th>Count</th><th>Failures</th><th>p50</th><th>p95</th><th>Max</th><th>Owner</th></tr>
              {{range .Comparison.Candidate.Routes}}
              <tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P50MS}} ms</td><td>{{.P95MS}} ms</td><td>{{.MaxMS}} ms</td><td><code>{{.OwnerSample}}</code></td></tr>
              {{else}}<tr><td colspan="7" class="muted">No HTTP events.</td></tr>{{end}}
            </table>
          </div>
          <div>
            <h3>Candidate Screens</h3>
            <table>
              <tr><th>Screen</th><th>Frames</th><th>Janky</th><th>Jank rate</th><th>Avg FPS</th><th>p95</th></tr>
              {{range .Comparison.Candidate.Screens}}
              <tr><td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{.JankyFrames}}</td><td>{{printf "%.2f" .JankRatePct}}%</td><td>{{printf "%.1f" .AvgFPS}}</td><td>{{.P95MS}} ms</td></tr>
              {{else}}<tr><td colspan="6" class="muted">No UI window events.</td></tr>{{end}}
            </table>
          </div>
        </div>
        <h3>Candidate Owners</h3>
        <table>
          <tr><th>Owner / Class</th><th>Kind</th><th>Count</th><th>Total</th><th>Max</th><th>Stack hint</th></tr>
          {{range .Comparison.Candidate.Owners}}
          <tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.TotalMS}} ms</td><td>{{.MaxMS}} ms</td><td><code>{{.StackHint}}</code></td></tr>
          {{else}}<tr><td colspan="6" class="muted">No owner attribution yet.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="drilldown" class="panel">
    <div class="panel-head">
      <div><h2>Per-log Drill-down</h2><div class="panel-kicker">Open any source log to inspect its own network, UI, memory, metrics and attribution profile.</div></div>
    </div>
    <h3>Baseline Logs</h3>
    {{range .BaselineLogs}}
    <details class="log-card" id="{{.Anchor}}">
      <summary>
        <div><strong class="mono-block">{{.Name}}</strong><div class="muted">{{.Summary.EventCount}} events · {{.Summary.DurationMS}} ms · {{.Summary.LogCount}} log</div></div>
        <div class="summary-metrics"><span class="pill">HTTP p95 {{.Summary.HTTPP95MS}} ms</span><span class="pill">Jank {{printf "%.2f" .Summary.UIJankPct}}%</span><span class="pill">FPS {{printf "%.1f" .Summary.UIAvgFPS}}</span></div>
      </summary>
      <div class="log-body">
        <div class="detail-grid">
          <div><h3>Routes</h3><table><tr><th>Route</th><th>Count</th><th>Failures</th><th>p95</th><th>Max</th></tr>{{range .Summary.Routes}}<tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P95MS}} ms</td><td>{{.MaxMS}} ms</td></tr>{{else}}<tr><td colspan="5" class="muted">No HTTP events.</td></tr>{{end}}</table></div>
          <div><h3>Screens</h3><table><tr><th>Screen</th><th>Frames</th><th>Jank</th><th>FPS</th><th>p95</th></tr>{{range .Summary.Screens}}<tr><td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{printf "%.2f" .JankRatePct}}%</td><td>{{printf "%.1f" .AvgFPS}}</td><td>{{.P95MS}} ms</td></tr>{{else}}<tr><td colspan="5" class="muted">No UI window events.</td></tr>{{end}}</table></div>
          <div><h3>Owners</h3><table><tr><th>Owner</th><th>Kind</th><th>Count</th><th>Max</th></tr>{{range .Summary.Owners}}<tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} ms</td></tr>{{else}}<tr><td colspan="4" class="muted">No owners.</td></tr>{{end}}</table></div>
          <div><h3>Memory And Metrics</h3><table><tr><th>Signal</th><th>Value</th><th>Details</th></tr><tr><td>max_pss_kb</td><td>{{.Summary.MemoryMaxKB}}</td><td>retained={{.Summary.Retained}}</td></tr>{{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{end}}</table></div>
        </div>
      </div>
    </details>
    {{else}}<div class="muted">No per-log baseline details were embedded.</div>{{end}}
    <h3>Candidate Logs</h3>
    {{range .CandidateLogs}}
    <details class="log-card" id="{{.Anchor}}">
      <summary>
        <div><strong class="mono-block">{{.Name}}</strong><div class="muted">{{.Summary.EventCount}} events · {{.Summary.DurationMS}} ms · {{.Summary.LogCount}} log</div></div>
        <div class="summary-metrics"><span class="pill">HTTP p95 {{.Summary.HTTPP95MS}} ms</span><span class="pill">Jank {{printf "%.2f" .Summary.UIJankPct}}%</span><span class="pill">FPS {{printf "%.1f" .Summary.UIAvgFPS}}</span></div>
      </summary>
      <div class="log-body">
        <div class="detail-grid">
          <div><h3>Routes</h3><table><tr><th>Route</th><th>Count</th><th>Failures</th><th>p95</th><th>Max</th></tr>{{range .Summary.Routes}}<tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P95MS}} ms</td><td>{{.MaxMS}} ms</td></tr>{{else}}<tr><td colspan="5" class="muted">No HTTP events.</td></tr>{{end}}</table></div>
          <div><h3>Screens</h3><table><tr><th>Screen</th><th>Frames</th><th>Jank</th><th>FPS</th><th>p95</th></tr>{{range .Summary.Screens}}<tr><td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{printf "%.2f" .JankRatePct}}%</td><td>{{printf "%.1f" .AvgFPS}}</td><td>{{.P95MS}} ms</td></tr>{{else}}<tr><td colspan="5" class="muted">No UI window events.</td></tr>{{end}}</table></div>
          <div><h3>Owners</h3><table><tr><th>Owner</th><th>Kind</th><th>Count</th><th>Max</th></tr>{{range .Summary.Owners}}<tr><td><code>{{.Owner}}</code></td><td>{{.Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} ms</td></tr>{{else}}<tr><td colspan="4" class="muted">No owners.</td></tr>{{end}}</table></div>
          <div><h3>Memory And Metrics</h3><table><tr><th>Signal</th><th>Value</th><th>Details</th></tr><tr><td>max_pss_kb</td><td>{{.Summary.MemoryMaxKB}}</td><td>retained={{.Summary.Retained}}</td></tr>{{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{end}}</table></div>
        </div>
      </div>
    </details>
    {{else}}<div class="muted">No per-log candidate details were embedded.</div>{{end}}
  </section>

  <section id="cohorts" class="panel">
    <div class="panel-head">
      <div><h2>Cohort Breakdown</h2><div class="panel-kicker">Use this to check whether the comparison is fair across app version, SDK, device, process and network.</div></div>
    </div>
    <details class="fold">
      <summary>Cohort Details</summary>
      <div class="fold-body">
        <div class="split">
          <div>
            <h3>Baseline</h3>
            <table><tr><th>Cohort</th><th>Events</th></tr>{{range .Comparison.Baseline.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No cohort metadata.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>Candidate</h3>
            <table><tr><th>Cohort</th><th>Events</th></tr>{{range .Comparison.Candidate.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">No cohort metadata.</td></tr>{{end}}</table>
          </div>
        </div>
        <h3>Process Mix</h3>
        <table>
          <tr><th>Baseline process</th><th>Sessions</th><th>Candidate process</th><th>Sessions</th></tr>
          <tr>
            <td>{{range .Comparison.Baseline.Processes}}<div><code>{{.Name}}</code></div>{{else}}<span class="muted">unknown</span>{{end}}</td>
            <td>{{range .Comparison.Baseline.Processes}}<div>{{.Value}}</div>{{else}}<span class="muted">0</span>{{end}}</td>
            <td>{{range .Comparison.Candidate.Processes}}<div><code>{{.Name}}</code></div>{{else}}<span class="muted">unknown</span>{{end}}</td>
            <td>{{range .Comparison.Candidate.Processes}}<div>{{.Value}}</div>{{else}}<span class="muted">0</span>{{end}}</td>
          </tr>
        </table>
      </div>
    </details>
  </section>

  <section id="analysis" class="panel">
    <div class="panel-head">
      <div><h2>Heuristic Verdict</h2><div class="panel-kicker">Rule-based triage over all comparison deltas and cohort warnings. Treat it as a review checklist, not as a mathematical proof.</div></div>
    </div>
    <div class="analysis-banner {{severityClass .Analysis.Severity}}">
      <div class="eyebrow">Overall status</div>
      <div class="analysis-status">{{.Analysis.Status}}</div>
      <div class="muted">{{.Analysis.Summary}}</div>
    </div>
    <h3>Findings</h3>
    <div class="finding-list">
      {{range .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">No heuristic findings.</div>{{end}}
    </div>
    <h3>Recommendations</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>No extra recommendations.</li>{{end}}
    </ul>
  </section>
</main>
</body>
</html>`
