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
  grid-template-columns: minmax(0, 1fr) minmax(360px, 460px);
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
  position: relative;
  overflow: hidden;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 54px;
  width: 100%;
  padding: 13px 18px;
  border: 1px solid rgba(98,255,168,0.74);
  border-radius: 999px;
  color: #03130a;
  background: linear-gradient(135deg, #62ffa8, #c9ff7a);
  font-weight: 950;
  letter-spacing: 0.02em;
  text-transform: none;
  animation: mathGlow 2.7s ease-in-out infinite;
}
.math-link::before {
  content: "Σ λ ∫ p95 Δt";
  position: absolute;
  inset: auto 18px 4px auto;
  color: rgba(3,19,10,0.16);
  font: 800 12px/1 "JetBrains Mono", "SFMono-Regular", Consolas, monospace;
  pointer-events: none;
}
.math-link:hover {
  color: #03130a;
  filter: brightness(1.08);
}
.hero-side { display: grid; gap: 10px; width: 100%; }
.hero-meta { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
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
.env-device { display: block; margin-top: 6px; font-size: 18px; line-height: 1.2; overflow-wrap: break-word; word-break: normal; }
.env-subtitle { margin-top: 2px; color: var(--muted); font-size: 12px; overflow-wrap: break-word; word-break: normal; }
.env-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; margin-top: 12px; }
.env-item {
  min-width: 0;
  padding: 9px;
  border: 1px solid rgba(126,247,255,0.13);
  border-radius: 8px;
  background: rgba(255,255,255,0.035);
}
.env-label { color: var(--muted); font-size: 10px; font-weight: 800; letter-spacing: 0.08em; text-transform: uppercase; }
.env-value { margin-top: 2px; font-weight: 850; overflow-wrap: break-word; word-break: normal; }
.env-detail { margin-top: 1px; color: var(--muted); font-size: 11px; overflow-wrap: break-word; word-break: normal; }
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
.nav a.active {
  border-color: rgba(98,255,168,0.62);
  color: var(--ok);
  box-shadow: 0 0 22px rgba(98,255,168,0.14);
}
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
  overflow-x: auto;
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
.help-text {
  margin: 8px 0 12px;
  color: var(--muted);
  font-size: 12px;
}
.explain {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border-bottom: 1px dashed rgba(111,247,255,0.58);
  color: inherit;
  cursor: help;
}
.explain::after {
  content: attr(data-tip);
  position: absolute;
  left: 0;
  bottom: calc(100% + 10px);
  z-index: 20;
  width: min(340px, 78vw);
  padding: 10px 12px;
  border: 1px solid rgba(111,247,255,0.45);
  border-radius: 8px;
  color: var(--ink);
  background:
    linear-gradient(135deg, rgba(111,247,255,0.12), rgba(255,79,216,0.10)),
    rgba(7,10,18,0.96);
  box-shadow: 0 18px 54px rgba(0,0,0,0.42), 0 0 22px rgba(111,247,255,0.16);
  font-size: 12px;
  line-height: 1.45;
  text-transform: none;
  letter-spacing: 0;
  white-space: normal;
  opacity: 0;
  transform: translateY(4px);
  pointer-events: none;
  transition: opacity 140ms ease, transform 140ms ease;
}
.explain:hover::after,
.explain:focus::after {
  opacity: 1;
  transform: translateY(0);
}
.metric .explain {
  color: var(--muted);
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
th, td { overflow-wrap: break-word; word-break: normal; hyphens: none; }
tr:hover td { background: rgba(111,247,255,0.035); }
.muted { color: var(--muted); }
code {
  color: #d8fcff;
  font-family: "JetBrains Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  overflow-wrap: break-word;
  word-break: normal;
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
  content: "открыть";
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.fold[open] > summary::after { content: "закрыть"; }
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
.mono-block { overflow-wrap: break-word; word-break: normal; }
.compare-pair-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(230px, 1fr));
  gap: 12px;
  margin: 12px 0 18px;
}
.compare-pair {
  min-width: 0;
  padding: 14px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255,255,255,0.032);
}
.compare-pair-title {
  color: var(--cyan);
  font-weight: 850;
  overflow-wrap: break-word;
  word-break: normal;
}
.compare-values {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  margin-top: 10px;
}
.compare-value {
  min-width: 0;
  padding: 8px;
  border: 1px solid rgba(126,247,255,0.12);
  border-radius: 8px;
  background: rgba(255,255,255,0.028);
}
.compare-value span {
  display: block;
  color: var(--muted);
  font-size: 11px;
  font-weight: 800;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.compare-value strong {
  display: block;
  margin-top: 3px;
  font-size: 18px;
  overflow-wrap: break-word;
  word-break: normal;
}
.compare-delta {
  margin-top: 8px;
  color: var(--muted);
  font-size: 12px;
}
.compare-table .metric-name {
  min-width: 170px;
  font-weight: 800;
}
.compare-table {
  min-width: 980px;
}
.compare-table th,
#changes table th,
#cohorts table th {
  white-space: nowrap;
}
#changes table {
  min-width: 1180px;
}
#cohorts table {
  min-width: 640px;
}
.compare-table td,
#changes table td,
#cohorts table td {
  overflow-wrap: normal;
  word-break: normal;
}
@media (max-width: 820px) {
  .hero { padding: 28px 18px 18px; }
  .hero-grid, .split, .triad, .detail-grid { grid-template-columns: 1fr; }
  .hero-side { width: 100%; }
  .hero-meta { grid-template-columns: 1fr; }
  .env-grid { grid-template-columns: 1fr; }
  main { padding: 18px; }
  .panel-head { display: block; }
  .chart-row { grid-template-columns: 1fr; }
  details.log-card summary { grid-template-columns: 1fr; }
  .summary-metrics { justify-content: flex-start; }
  table { min-width: 680px; }
  h1 { font-size: clamp(34px, 12vw, 52px); }
  .metric .value { font-size: 23px; }
}
@media (max-width: 520px) {
  body { font-size: 13px; }
  .nav { padding: 10px 12px; }
  main { padding: 12px; }
  .panel, .log-card { padding: 14px; }
  .hero { padding: 22px 14px 16px; }
  .grid { grid-template-columns: 1fr; }
  .compare-values { grid-template-columns: 1fr; }
  h1 { font-size: clamp(32px, 9vw, 40px); line-height: 1; }
}
`

const mathCSS = `
.math-page .fold > summary::after { content: "открыть"; }
.math-page .fold[open] > summary::after { content: "закрыть"; }
.math-page .nav {
  box-shadow: 0 14px 36px rgba(0,0,0,0.22);
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
  overflow-wrap: break-word;
  word-break: normal;
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
  display: block;
  margin-bottom: 8px;
}
.timeline-chart-title {
  font-weight: 850;
  overflow-wrap: normal;
  word-break: normal;
  hyphens: none;
}
.timeline-chart-value { margin-top: 3px; color: var(--muted); font-size: 12px; white-space: normal; }
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
.method-reference-card summary span:first-child {
  overflow-wrap: break-word;
  word-break: normal;
}
.method-kind { color: var(--ok); }
.zero-toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  margin: 4px 0 12px;
  color: var(--muted);
  font-size: 12px;
}
.zero-toggle input { accent-color: #62ffa8; }
.bucket-zero { display: none; }
.show-zero-buckets .bucket-zero { display: table-row; }
.category-block { margin: 16px 0 22px; }
.category-block h4 {
  margin: 0 0 8px;
  color: var(--ink);
  font-size: 13px;
}
.causal-graph-card {
  margin: 10px 0 16px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background:
    linear-gradient(135deg, rgba(111,247,255,0.08), rgba(255,79,216,0.06)),
    rgba(255,255,255,0.026);
  overflow-x: auto;
}
.causal-graph {
  width: 100%;
  min-width: 760px;
  height: auto;
  display: block;
}
.causal-edge { stroke: rgba(111,247,255,0.48); stroke-width: 1.5; }
.causal-node rect {
  fill: rgba(13,24,43,0.94);
  stroke: rgba(126,247,255,0.38);
  rx: 8;
}
.causal-node text {
  fill: var(--ink);
  font: 700 11px Inter, "SF Pro Text", Arial, sans-serif;
}
.causal-node .kind {
  fill: var(--muted);
  font-size: 9px;
  text-transform: uppercase;
}
.heuristic-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}
.heuristic-card {
  border: 1px solid var(--line);
  border-left: 4px solid var(--ok);
  border-radius: 8px;
  padding: 12px;
  background: rgba(255,255,255,0.032);
}
.heuristic-card.sev-high { border-left-color: var(--bad); }
.heuristic-card.sev-medium { border-left-color: var(--warn); }
.heuristic-card strong { display: block; margin-bottom: 4px; }
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
  .timeline-grid { grid-template-columns: 1fr; }
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
        <div class="env-subtitle">{{.Math.Summary.LogCount}} логов · {{humanDuration .Math.Summary.DurationMS}} · {{.Math.Summary.HTTPCount}} HTTP</div>
        <div class="env-grid">
          <div class="env-item"><div class="env-label">UI-кадры</div><div class="env-value">{{.Math.Summary.UIFrames}}</div><div class="env-detail">медленных {{.Math.Summary.UIJank}}</div></div>
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
        <div class="metric"><div class="label">{{tip "Подтормаживания UI" "Доля кадров, которые были медленнее целевого времени кадра. Это основной пользовательский симптом визуальной просадки."}}</div><div class="value">{{printf "%.2f" .Math.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Summary.UIJank}} / {{.Math.Summary.UIFrames}} кадров</div></div>
        <div class="metric"><div class="label">{{tip "Память" "Максимальный PSS: пропорциональный размер памяти процесса с учетом разделяемых страниц."}}</div><div class="value">{{.Math.Summary.MemoryMaxKB}} KB</div><div class="hint">макс. PSS</div></div>
        <div class="metric"><div class="label">{{tip "Флоу" "Количество кортежей контекста флоу: экран, флоу, шаг и источник работ. Это связывает математические сигналы с пользовательским сценарием."}}</div><div class="value">{{len .Math.Summary.Flows}}</div><div class="hint">проблем {{summaryProblems .Math.Summary}}, спам {{summaryLogSpam .Math.Summary}}</div></div>
      </div>
      <div>
        <h3>Исходные логи</h3>
        <div class="source-list">{{range .Math.SourcePaths}}<code>{{.}}</code>{{else}}<span class="muted">Исходные логи не указаны.</span>{{end}}</div>
      </div>
    </div>
    <h3>Атрибуция флоу и причин</h3>
    <table class="timeline-table">
      <tr><th>Экран / флоу / шаг / источник</th><th>Маршрут</th><th>HTTP</th><th>HTTP p95</th><th>UI подтормаживания</th><th>Паузы</th><th>Спам логами</th><th>Проблемы</th><th>Макс. PSS</th></tr>
      {{range .Math.Summary.Flows}}
      <tr><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td><td><code>{{.RouteSample}}</code></td><td>{{.HTTPCount}}</td><td>{{.HTTPP95MS}} мс</td><td>{{.UIJank}} / {{.UIFrames}} · {{printf "%.2f" .UIJankPct}}%</td><td>{{.StallCount}} · макс. {{.StallMaxMS}} мс</td><td>{{.LogSpam}}</td><td>{{.ProblemCount}}</td><td>{{.MemoryMaxKB}} KB</td></tr>
      {{else}}<tr><td colspan="9" class="muted">Нет событий контекста флоу. Для причинной математики включите API флоу или ASM-опцию flowInteractions.</td></tr>{{end}}
    </table>
    <div class="split">
      <div>
        <h3>Спам логами</h3>
        <table class="timeline-table"><tr><th>Источник</th><th>Уровень</th><th>Количество</th><th>Контекст</th></tr>{{range .Math.Summary.LogSpam}}<tr><td><code>{{.Source}}</code></td><td>{{.Level}}</td><td>{{.Count}}</td><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td></tr>{{else}}<tr><td colspan="4" class="muted">Нет событий спама логами.</td></tr>{{end}}</table>
      </div>
      <div>
        <h3>Проблемные окна</h3>
        <table class="timeline-table"><tr><th>Причина</th><th>Окна</th><th>Счетчик</th><th>Итого окно</th><th>Макс.</th><th>Контекст</th></tr>{{range .Math.Summary.ProblemWindows}}<tr><td>{{problemKind .Kind}}</td><td>{{.Windows}}</td><td>{{.Count}}</td><td>{{humanDuration .TotalWindowMS}}</td><td>{{.MaxMS}} мс</td><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td></tr>{{else}}<tr><td colspan="6" class="muted">Нет агрегированных проблемных окон.</td></tr>{{end}}</table>
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
        <h3>{{tip "Временные интервалы" "Внутренний термин: бакет. Это фиксированное окно времени, например 1 секунда, куда складываются события для анализа временного ряда."}}</h3>
        <label class="zero-toggle"><input type="checkbox" data-zero-toggle>Показать нулевые интервалы</label>
        <div class="category-block">
          <h4>Сеть</h4>
          <table class="timeline-table">
            <tr><th>Время</th><th>HTTP</th><th>Ошибки</th><th>HTTP средн.</th><th>HTTP p95</th><th>DNS кол-во</th><th>DNS средн.</th><th>Connect кол-во</th><th>Connect средн.</th><th>TTFB средн.</th></tr>
            {{range $math.Timeline}}
            <tr class="{{bucketClass .}}"><td>{{bucketRange .}}</td><td>{{.HTTPCount}}</td><td>{{.HTTPFailed}}</td><td>{{.HTTPAvgDurationMS}} мс</td><td>{{.HTTPP95DurationMS}} мс</td><td>{{.DNSCount}}</td><td>{{.DNSDurationMS}} мс</td><td>{{.ConnectCount}}</td><td>{{.ConnectDurationMS}} мс</td><td>{{.TTFBMS}} мс</td></tr>
            {{else}}<tr><td colspan="10" class="muted">Недостаточно данных для надежного анализа.</td></tr>{{end}}
          </table>
        </div>
        <div class="category-block">
          <h4>UI и главный поток</h4>
          <table class="timeline-table">
          <tr><th>Время</th><th>UI кадры</th><th>Медленные кадры</th><th>Доля подтормаживаний</th><th>Паузы главного потока</th><th>Макс. пауза</th></tr>
            {{range $math.Timeline}}
            <tr class="{{bucketClass .}}"><td>{{bucketRange .}}</td><td>{{.UIFrames}}</td><td>{{.UIJankyFrames}}</td><td>{{printf "%.2f" (jankPct .UIJankyFrames .UIFrames)}}%</td><td>{{.StallCount}}</td><td>{{.StallMaxMS}} мс</td></tr>
            {{else}}<tr><td colspan="6" class="muted">Недостаточно данных для надежного анализа.</td></tr>{{end}}
          </table>
        </div>
        <div class="category-block">
          <h4>Память и трафик</h4>
          <table class="timeline-table">
            <tr><th>Время</th><th>{{tip "PSS" "Пропорциональный размер памяти процесса с учетом разделяемых страниц."}}</th><th>Свободная RAM</th><th>RX дельта</th><th>TX дельта</th></tr>
            {{range $math.Timeline}}
            <tr class="{{bucketClass .}}"><td>{{bucketRange .}}</td><td>{{.MemoryPSSKB}} KB</td><td>{{.AvailableMemoryKB}} KB</td><td>{{.TrafficRxBytes}}</td><td>{{.TrafficTxBytes}}</td></tr>
            {{else}}<tr><td colspan="5" class="muted">Недостаточно данных для надежного анализа.</td></tr>{{end}}
          </table>
        </div>
        {{end}}
        {{if eq .ID "robust"}}
        <h3>Распределения</h3>
        {{range robustGroups $math.RobustStats}}
        <div class="category-block">
          <h4>{{.Title}}</h4>
          <table class="timeline-table">
            <tr><th>Имя</th><th>Метрика</th><th>N</th><th>Медиана</th><th>p90</th><th>p95</th><th>p99</th><th>MAD</th><th>Усеченное среднее</th><th>Мин.</th><th>Макс.</th><th>Интервал p95</th><th>Качество</th></tr>
            {{range .Items}}
            <tr>
              <td><code>{{.Name}}</code></td><td>{{.Metric}}</td><td>{{.Count}}</td>
              <td>{{printf "%.1f" .Median}} {{.Unit}}</td><td>{{printf "%.1f" .P90}} {{.Unit}}</td><td>{{printf "%.1f" .P95}} {{.Unit}}</td><td>{{printf "%.1f" .P99}} {{.Unit}}</td>
              <td>{{printf "%.1f" .MAD}} {{.Unit}}</td><td>{{printf "%.1f" .TrimmedMean}} {{.Unit}}</td><td>{{printf "%.1f" .Min}} {{.Unit}}</td><td>{{printf "%.1f" .Max}} {{.Unit}}</td>
              <td>{{if .HasP95Confidence}}{{printf "%.1f" .P95ConfidenceLow}}..{{printf "%.1f" .P95ConfidenceHigh}} {{.Unit}}{{else}}мало данных{{end}}</td>
              <td><span class="section-status {{severityClass .SampleQualitySeverity}}">{{.SampleQuality}}</span><div class="muted">{{.SampleDetail}}</div></td>
            </tr>
            {{end}}
          </table>
        </div>
        {{else}}<div class="muted">Недостаточно данных для робастной статистики.</div>{{end}}
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
        <p class="muted">Интегрирование прямоугольное: для каждого временного интервала берется значение сигнала и умножается на длительность интервала Δt. Чем дольше длится деградация, тем больше итоговая площадь.</p>
        {{end}}
        {{if eq .ID "markov"}}
        <h3>Сводка состояний</h3>
        <table class="timeline-table">
          <tr><th>Здоровые -> плохие</th><th>Восстановление</th><th>Ожидаемое восстановление</th><th>Липкие состояния</th></tr>
          <tr>
            <td>{{$math.Markov.HealthyToBadCount}}</td>
            <td>{{printf "%.1f" (percent01 $math.Markov.BadToHealthyProbability)}}%</td>
            <td>{{printf "%.1f" $math.Markov.ExpectedRecoveryWindows}} временных интервалов</td>
            <td>{{range $math.Markov.StickyStates}}<div>{{markovState .State}} · {{printf "%.1f" (percent01 .Probability)}}% · {{.Count}} переходов</div>{{else}}<span class="muted">нет</span>{{end}}</td>
          </tr>
        </table>
        <h3>Последовательность временных интервалов</h3>
        <table class="timeline-table">
          <tr><th>Время</th><th>Состояние</th><th>Причина</th><th>Контекст</th></tr>
          {{range $math.Markov.States}}
          <tr>
            <td>{{printf "%.1fs" (seconds .TimeMS)}}</td>
            <td>{{markovState .State}}</td>
            <td>{{.Reason}}</td>
            <td>{{if .Screen}}экран <code>{{.Screen}}</code><br>{{end}}{{if .Route}}маршрут <code>{{.Route}}</code><br>{{end}}{{if .Owner}}источник <code>{{.Owner}}</code><br>{{end}}{{if .Network}}сеть <code>{{.Network}}</code>{{end}}</td>
          </tr>
          {{else}}<tr><td colspan="4" class="muted">Недостаточно временных интервалов для марковской модели.</td></tr>{{end}}
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
        <h3>Визуальный граф</h3>
        <p class="help-text">Показаны только самые сильные связи, чтобы большой проект не превращал граф в шум. Полные ребра и пути доступны в таблицах ниже.</p>
        {{causalGraphSVG $math.CausalGraph}}
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
          <div class="reference-block"><strong>Где применяется</strong>{{.AppliesTo}}</div>
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
  {{with mathHeuristic .Math}}
  <section id="math-verdict" class="panel">
    <div class="panel-head">
      <div>
        <h2>Итоговая эвристика</h2>
        <div class="panel-kicker">Сводка по всем математическим разделам: общий диагноз, главные факторы риска и первый шаг расследования.</div>
      </div>
      <span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span>
    </div>
    <div class="analysis-banner {{severityClass .Severity}}">
      <div class="eyebrow">Общий вывод</div>
      <div class="analysis-status">{{.Status}}</div>
      <div class="muted">{{.Summary}}</div>
    </div>
    <div class="heuristic-grid">
      {{range .Cards}}
      <div class="heuristic-card {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{end}}
    </div>
  </section>
  {{end}}
</main>
<script>
(() => {
  const links = Array.from(document.querySelectorAll('.math-page .nav a'));
  const setActive = (hash) => {
    links.forEach((link) => link.classList.toggle('active', link.getAttribute('href') === hash));
  };
  links.forEach((link) => link.addEventListener('click', () => setActive(link.getAttribute('href'))));
  if (location.hash) setActive(location.hash);
  const sections = links.map((link) => document.querySelector(link.getAttribute('href'))).filter(Boolean);
  if ('IntersectionObserver' in window) {
    const observer = new IntersectionObserver((entries) => {
      const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
      if (visible) setActive('#' + visible.target.id);
    }, { rootMargin: '-20% 0px -65% 0px', threshold: [0.1, 0.3, 0.6] });
    sections.forEach((section) => observer.observe(section));
  }
  document.querySelectorAll('[data-zero-toggle]').forEach((input) => {
    input.addEventListener('change', () => document.body.classList.toggle('show-zero-buckets', input.checked));
  });
})();
</script>
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
          <div class="env-item"><div class="env-label">HTTP базы</div><div class="env-value">{{.Math.Baseline.Summary.HTTPCount}}</div><div class="env-detail">p95 {{.Math.Baseline.Summary.HTTPP95MS}} мс</div></div>
          <div class="env-item"><div class="env-label">HTTP кандидата</div><div class="env-value">{{.Math.Candidate.Summary.HTTPCount}}</div><div class="env-detail">p95 {{.Math.Candidate.Summary.HTTPP95MS}} мс</div></div>
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
      <div class="metric"><div class="label">Подтормаживания базы</div><div class="value">{{printf "%.2f" .Math.Baseline.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Baseline.Summary.UIFrames}} кадров</div></div>
      <div class="metric"><div class="label">Подтормаживания кандидата</div><div class="value">{{printf "%.2f" .Math.Candidate.Summary.UIJankPct}}%</div><div class="hint">{{.Math.Candidate.Summary.UIFrames}} кадров</div></div>
      <div class="metric"><div class="label">{{tip "Проблемные окна" "Агрегированные окна причин: медленный HTTP, пауза главного потока, UI-подтормаживания, удержания или спам логами."}}</div><div class="value">{{summaryProblems .Math.Baseline.Summary}} → {{summaryProblems .Math.Candidate.Summary}}</div><div class="hint">спам {{summaryLogSpam .Math.Baseline.Summary}} → {{summaryLogSpam .Math.Candidate.Summary}}</div></div>
    </div>
    <h3>Сравнение флоу и причин</h3>
    <table class="timeline-table">
      <tr><th>Контекст</th><th>Проблемы базы</th><th>Проблемы кандидата</th><th>Δ проблем</th><th>Спам базы</th><th>Спам кандидата</th><th>Δ спама</th><th>HTTP p95 база</th><th>HTTP p95 кандидат</th><th>UI база</th><th>UI кандидат</th><th>Серьезность</th></tr>
      {{range flowCompareRows .Math.Baseline.Summary .Math.Candidate.Summary}}
      <tr>
        <td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td>
        <td>{{.BaselineProblems}}</td><td>{{.CandidateProblems}}</td><td>{{.DeltaProblems}}</td>
        <td>{{.BaselineLogSpam}}</td><td>{{.CandidateLogSpam}}</td><td>{{.DeltaLogSpam}}</td>
        <td>{{.BaselineHTTPP95MS}} мс</td><td>{{.CandidateHTTPP95MS}} мс</td>
        <td>{{printf "%.2f" .BaselineJankPct}}%</td><td>{{printf "%.2f" .CandidateJankPct}}%</td>
        <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
      </tr>
      {{else}}<tr><td colspan="12" class="muted">Нет событий контекста флоу для математического сравнения.</td></tr>{{end}}
    </table>
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
        {{range robustDeltaGroups $math.RobustDeltas}}
        <div class="category-block">
          <h4>{{.Title}}</h4>
          <table class="timeline-table">
            <tr><th>Статус</th><th>Имя</th><th>Метрика</th><th>N база</th><th>N кандидат</th><th>p95 база</th><th>p95 кандидат</th><th>Δ p95</th><th>Δ%</th><th>Дельта Клиффа</th><th>Эффект</th><th>Доверие</th><th>Вывод</th></tr>
            {{range .Items}}
            <tr>
              <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
              <td><code>{{.Name}}</code></td><td>{{.Metric}}</td><td>{{.BaselineCount}}</td><td>{{.CandidateCount}}</td>
              <td>{{printf "%.1f" .BaselineP95}} {{.Unit}}</td><td>{{printf "%.1f" .CandidateP95}} {{.Unit}}</td><td>{{printf "%+.1f" .P95Delta}} {{.Unit}}</td><td>{{printf "%+.1f" .P95DeltaPct}}%</td>
              <td>{{printf "%.3f" .CliffDelta}}</td><td>{{.EffectSize}}</td><td>{{.Confidence}}</td><td>{{.Summary}}</td>
            </tr>
            {{end}}
          </table>
        </div>
        {{else}}<div class="muted">Недостаточно пересекающихся распределений для робастного сравнения.</div>{{end}}
        <p class="muted">Положительная дельта Клиффа означает, что значения кандидата чаще больше базы. Для задержек, подтормаживаний UI, памяти и очередей это обычно ухудшение.</p>
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
        <h3>Дельты марковских метрик</h3>
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
          {{else}}<tr><td colspan="6" class="muted">Марковские дельты недоступны.</td></tr>{{end}}
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
        <h3>Визуальный граф кандидата</h3>
        <p class="help-text">Показаны самые сильные связи кандидата. Полная детализация ребер остается в таблицах ниже.</p>
        {{causalGraphSVG $math.Candidate.CausalGraph}}
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
          <div class="reference-block"><strong>Где применяется</strong>{{.AppliesTo}}</div>
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
  {{with compareMathHeuristic .Math}}
  <section id="math-verdict" class="panel">
    <div class="panel-head">
      <div>
        <h2>Итоговая эвристика сравнения</h2>
        <div class="panel-kicker">Сводка по математическим дельтам: общий риск кандидата, главные факторы и первый шаг расследования.</div>
      </div>
      <span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span>
    </div>
    <div class="analysis-banner {{severityClass .Severity}}">
      <div class="eyebrow">Общий вывод</div>
      <div class="analysis-status">{{.Status}}</div>
      <div class="muted">{{.Summary}}</div>
    </div>
    <div class="heuristic-grid">
      {{range .Cards}}
      <div class="heuristic-card {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{end}}
    </div>
  </section>
  {{end}}
</main>
<script>
(() => {
  const links = Array.from(document.querySelectorAll('.math-page .nav a'));
  const setActive = (hash) => {
    links.forEach((link) => link.classList.toggle('active', link.getAttribute('href') === hash));
  };
  links.forEach((link) => link.addEventListener('click', () => setActive(link.getAttribute('href'))));
  if (location.hash) setActive(location.hash);
  const sections = links.map((link) => document.querySelector(link.getAttribute('href'))).filter(Boolean);
  if ('IntersectionObserver' in window) {
    const observer = new IntersectionObserver((entries) => {
      const visible = entries.filter((entry) => entry.isIntersecting).sort((a, b) => b.intersectionRatio - a.intersectionRatio)[0];
      if (visible) setActive('#' + visible.target.id);
    }, { rootMargin: '-20% 0px -65% 0px', threshold: [0.1, 0.3, 0.6] });
    sections.forEach((section) => observer.observe(section));
  }
  document.querySelectorAll('[data-zero-toggle]').forEach((input) => {
    input.addEventListener('change', () => document.body.classList.toggle('show-zero-buckets', input.checked));
  });
})();
</script>
</body>
</html>`

const inspectTemplate = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter: отчет</title>
  <style>` + baseCSS + `</style>
</head>
<body>
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter · обзор</div>
      <h1>Отчет по сигналам выполнения</h1>
      <div class="subhead">{{.Summary.Title}} · создан {{.GeneratedAt}} · автономный HTML</div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Контекст устройства</div>
        <strong class="env-device">{{fallback .Summary.Environment.Title "неизвестное устройство"}}</strong>
        <div class="env-subtitle">{{fallback .Summary.Environment.Subtitle "контекст выполнения недоступен"}}</div>
        <div class="env-grid">
          {{range .Summary.Environment.Items}}
          <div class="env-item"><div class="env-label">{{.Label}}</div><div class="env-value">{{.Value}}</div><div class="env-detail">{{.Detail}}</div></div>
          {{else}}<div class="env-item"><div class="env-label">Контекст</div><div class="env-value">неизвестно</div><div class="env-detail">Нет метаданных сессии и контекста.</div></div>{{end}}
        </div>
      </div>
      <div class="hero-meta">
        <div class="chip">Логи <strong>{{.Summary.LogCount}}</strong></div>
        <div class="chip">События <strong>{{.Summary.EventCount}}</strong></div>
        <div class="chip">Длительность <strong>{{humanDuration .Summary.DurationMS}}</strong></div>
      </div>
      <div class="hero-actions"><a class="math-link" href="{{.MathReportHref}}">λ Анализ</a></div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#overview">Обзор</a>
  <a href="#network">Сеть</a>
  <a href="#ui">UI</a>
  <a href="#flows">Флоу</a>
  <a href="#owners">Источники</a>
  <a href="#memory">Память</a>
  <a href="#custom">Метрики</a>
  <a href="#context">Контекст</a>
  <a href="#analysis">Итог</a>
</nav>
<main>
  <section id="overview" class="panel">
    <div class="panel-head">
      <div>
        <h2>Матрица ключевых сигналов</h2>
        <div class="panel-kicker">Быстрый срез прогона: задержки, плавность, паузы главного потока, память и трафик.</div>
      </div>
      <span class="pill">автономный отчет</span>
    </div>
    <div class="grid">
      <div class="metric"><div class="label">{{tip "HTTP p95" "95-й процентиль длительности HTTP-запросов: 95% запросов были не медленнее этого значения, а худшие 5% — медленнее."}}</div><div class="value">{{.Summary.HTTPP95MS}} мс</div><div class="hint">{{.Summary.HTTPCount}} запросов, ошибок {{.Summary.HTTPFailed}}</div></div>
      <div class="metric"><div class="label">{{tip "Подтормаживания UI" "Доля кадров, которые были медленнее целевого времени кадра. Чем выше процент, тем заметнее рывки интерфейса."}}</div><div class="value">{{printf "%.2f" .Summary.UIJankPct}}%</div><div class="hint">{{.Summary.UIJank}} / {{.Summary.UIFrames}} кадров</div></div>
      <div class="metric"><div class="label">{{tip "Средний FPS" "Frames per second: среднее число UI-кадров в секунду. Для плавного интерфейса обычно стремятся к 60 FPS или выше на 60 Hz экранах."}}</div><div class="value">{{printf "%.1f" .Summary.UIAvgFPS}}</div><div class="hint">минимум {{printf "%.1f" .Summary.UIMinFPS}}</div></div>
      <div class="metric"><div class="label">{{tip "Макс. пауза" "Самая длинная задержка работы на главном потоке. Длинные паузы блокируют обработку ввода и отрисовку."}}</div><div class="value">{{.Summary.StallMaxMS}} мс</div><div class="hint">событий пауз {{.Summary.StallCount}}</div></div>
      <div class="metric"><div class="label">{{tip "Макс. PSS" "PSS — пропорциональный размер памяти процесса. Учитывает долю разделяемых страниц и показывает реальный вклад приложения в RAM."}}</div><div class="value">{{.Summary.MemoryMaxKB}} KB</div><div class="hint">удержано {{.Summary.Retained}}</div></div>
      <div class="metric"><div class="label">{{tip "Макс. RX UID" "Максимальный принятый сетевой трафик UID приложения по снимкам контекста. Помогает увидеть тяжелую сетевую активность."}}</div><div class="value">{{.Summary.TrafficRxMax}}</div><div class="hint">макс. TX {{.Summary.TrafficTxMax}}</div></div>
    </div>
    <h3>Индикаторы здоровья</h3>
    <div class="ring-row">
      <div class="gauge" style="{{ringStyle .Summary.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIJankPct}}%</strong><span>UI-подтормаживания</span></div></div></div>
      <div class="gauge" style="{{ringStyle (rate .Summary.HTTPFailed .Summary.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Summary.HTTPFailed .Summary.HTTPCount)}}%</strong><span>HTTP ошибки</span></div></div></div>
      <div class="gauge" style="{{ringStyle (fpsScore .Summary.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIAvgFPS}}</strong><span>средний FPS</span></div></div></div>
    </div>
  </section>

  <section id="network" class="panel">
    <div class="panel-head">
      <div><h2>Сетевые маршруты</h2><div class="panel-kicker">Самые медленные маршруты по p95-задержке, ошибкам, байтам и влиянию источников.</div></div>
    </div>
    <details class="fold">
      <summary>Детали маршрутов</summary>
      <div class="fold-body">
        <div class="chart-list">
          {{range .Summary.Routes}}
          <div class="chart-row"><code>{{.Route}}</code><div class="chart-track"><i style="{{msWidth .P95MS}}"></i></div><strong>{{.P95MS}} мс</strong></div>
          {{else}}<div class="muted">Нет HTTP-событий.</div>{{end}}
        </div>
        <h3>Таблица маршрутов</h3>
        <table>
          <tr><th>Маршрут</th><th>Количество</th><th>Ошибки</th><th>p50</th><th>p95</th><th>Макс.</th><th>Средний TTFB</th><th>RX</th><th>TX</th><th>Источник</th></tr>
          {{range .Summary.Routes}}
          <tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P50MS}} мс</td><td>{{.P95MS}} мс</td><td>{{.MaxMS}} мс</td><td>{{.AvgTTFBMS}} мс</td><td>{{.BytesRx}}</td><td>{{.BytesTx}}</td><td><code>{{.OwnerSample}}</code></td></tr>
          {{else}}<tr><td colspan="10" class="muted">Нет HTTP-событий.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="ui" class="panel">
    <div class="panel-head">
      <div><h2>Плавность UI</h2><div class="panel-kicker">Экраны, отсортированные по доле подтормаживаний и задержке кадров.</div></div>
    </div>
    <details class="fold">
      <summary>Детали экранов</summary>
      <div class="fold-body">
        <div class="chart-list">
          {{range .Summary.Screens}}
          <div class="chart-row"><code>{{.Screen}}</code><div class="chart-track warn"><i style="{{pctWidth .JankRatePct}}"></i></div><strong>{{printf "%.2f" .JankRatePct}}%</strong></div>
          {{else}}<div class="muted">Нет событий UI-окон.</div>{{end}}
        </div>
        <h3>Таблица экранов</h3>
        <table>
          <tr><th>Экран</th><th>Окна</th><th>Кадры</th><th>Медленные кадры</th><th>Доля подтормаживаний</th><th>Средний FPS</th><th>Мин. FPS</th><th>p95 кадра</th><th>Макс. p99</th></tr>
          {{range .Summary.Screens}}
          <tr>
            <td><code>{{.Screen}}</code></td><td>{{.WindowCount}}</td><td>{{.Frames}}</td><td>{{.JankyFrames}}</td>
            <td><div>{{printf "%.2f" .JankRatePct}}%</div><div class="bar"><i style="{{pctWidth .JankRatePct}}"></i></div></td>
            <td>{{printf "%.1f" .AvgFPS}}</td><td>{{printf "%.1f" .MinFPS}}</td><td>{{.P95MS}} мс</td><td>{{.MaxP99MS}} мс</td>
          </tr>
          {{else}}<tr><td colspan="9" class="muted">Нет событий UI-окон.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="flows" class="panel">
    <div class="panel-head">
      <div><h2>Флоу и причины</h2><div class="panel-kicker">Связка экран → флоу → шаг → источник показывает, где совпали сеть, паузы, UI, память и спам логами.</div></div>
    </div>
    <details class="fold" open>
      <summary>Детали флоу</summary>
      <div class="fold-body">
        <table>
          <tr><th>Экран</th><th>Флоу</th><th>Шаг</th><th>Источник</th><th>Маршрут</th><th>HTTP</th><th>Ошибки</th><th>HTTP p95</th><th>UI подтормаживания</th><th>Паузы</th><th>Макс. пауза</th><th>Спам логами</th><th>Проблемы</th><th>Макс. PSS</th></tr>
          {{range .Summary.Flows}}
          <tr>
            <td><code>{{.Screen}}</code></td>
            <td><code>{{.Flow}}</code></td>
            <td><code>{{.Step}}</code></td>
            <td><code>{{.Owner}}</code></td>
            <td><code>{{.RouteSample}}</code></td>
            <td>{{.HTTPCount}}</td>
            <td>{{.HTTPFailed}}</td>
            <td>{{.HTTPP95MS}} мс</td>
            <td>{{.UIJank}} / {{.UIFrames}} · {{printf "%.2f" .UIJankPct}}%</td>
            <td>{{.StallCount}}</td>
            <td>{{.StallMaxMS}} мс</td>
            <td>{{.LogSpam}}</td>
            <td>{{.ProblemCount}}</td>
            <td>{{.MemoryMaxKB}} KB</td>
          </tr>
          {{else}}<tr><td colspan="14" class="muted">Нет событий контекста флоу. Включите API флоу или ASM-опцию flowInteractions, чтобы увидеть цепочки причин.</td></tr>{{end}}
        </table>

        <div class="split">
          <div>
            <h3>{{tip "Спам логами" "Счетчик частых вызовов android.util.Log.* и Timber.*. Текст логов не сохраняется: пишется только источник, уровень, контекст и количество вызовов."}}</h3>
            <table>
              <tr><th>Источник</th><th>Уровень</th><th>Количество</th><th>Экран</th><th>Флоу</th><th>Шаг</th><th>Источник работ</th></tr>
              {{range .Summary.LogSpam}}
              <tr><td><code>{{.Source}}</code></td><td>{{.Level}}</td><td>{{.Count}}</td><td><code>{{.Screen}}</code></td><td><code>{{.Flow}}</code></td><td><code>{{.Step}}</code></td><td><code>{{.Owner}}</code></td></tr>
              {{else}}<tr><td colspan="7" class="muted">Нет событий спама логами.</td></tr>{{end}}
            </table>
          </div>
          <div>
            <h3>{{tip "Проблемные окна" "Агрегированные окна, где уже заметна причина: медленный HTTP, пауза главного потока, UI-подтормаживания, удержанные объекты или спам логами."}}</h3>
            <table>
              <tr><th>Причина</th><th>Окна</th><th>Счетчик</th><th>Итого окно</th><th>Макс.</th><th>Экран</th><th>Флоу</th><th>Шаг</th><th>Источник</th></tr>
              {{range .Summary.ProblemWindows}}
              <tr><td>{{problemKind .Kind}}</td><td>{{.Windows}}</td><td>{{.Count}}</td><td>{{humanDuration .TotalWindowMS}}</td><td>{{.MaxMS}} мс</td><td><code>{{.Screen}}</code></td><td><code>{{.Flow}}</code></td><td><code>{{.Step}}</code></td><td><code>{{.Owner}}</code></td></tr>
              {{else}}<tr><td colspan="9" class="muted">Нет агрегированных проблемных окон.</td></tr>{{end}}
            </table>
          </div>
        </div>
      </div>
    </details>
  </section>

  <section id="owners" class="panel">
    <div class="panel-head">
      <div><h2>Горячие точки влияния</h2><div class="panel-kicker">Источники, классы и подсказки стека с наибольшим измеренным вкладом.</div></div>
    </div>
    <details class="fold">
      <summary>Детали источников</summary>
      <div class="fold-body">
        <table>
          <tr><th>Источник / класс</th><th>Тип</th><th>Количество</th><th>Итого</th><th>Макс.</th><th>Подсказка стека</th></tr>
          {{range .Summary.Owners}}
          <tr><td><code>{{.Owner}}</code></td><td>{{ownerKind .Kind}}</td><td>{{.Count}}</td><td>{{.TotalMS}} мс</td><td>{{.MaxMS}} мс</td><td><code>{{.StackHint}}</code></td></tr>
          {{else}}<tr><td colspan="6" class="muted">Атрибуция источников пока недоступна.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="memory" class="panel">
    <div class="panel-head">
      <div><h2>Память и удержанные объекты</h2><div class="panel-kicker">PSS, свободная память, сигналы низкой памяти и возраст удержанных объектов.</div></div>
    </div>
    <details class="fold">
      <summary>Детали памяти</summary>
      <div class="fold-body">
        <div class="split">
          <div>
            <h3>Память</h3>
            <p class="help-text">{{tip "PSS" "Proportional Set Size: пропорциональный размер памяти процесса. Разделяемые страницы учитываются только частично, поэтому PSS хорошо показывает вклад приложения в потребление RAM."}} · {{tip "Давление памяти" "Состояние, когда свободной RAM мало или память приложения быстро растет. Это повышает риск частого GC, вытеснения кэшей и убийства процесса системой."}}</p>
            <table>
              <tr><th>Метрика</th><th>Значение</th><th>Детали</th><th>Пояснение</th></tr>
              {{range .Summary.Memory}}<tr><td>{{tip .Name (memoryHelp .Name)}}</td><td>{{.Value}}</td><td>{{.Extra}}</td><td class="muted">{{memoryHelp .Name}}</td></tr>{{else}}<tr><td colspan="4" class="muted">Нет событий памяти.</td></tr>{{end}}
            </table>
          </div>
          <div>
            <h3>Удержанные классы</h3>
            <table>
              <tr><th>Класс / источник</th><th>Количество</th><th>Детали</th></tr>
              {{range .Summary.RetainedClasses}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Нет событий удержанных объектов.</td></tr>{{end}}
            </table>
          </div>
        </div>
        <h3>Возраст удержанных объектов</h3>
        <table>
          <tr><th>Возраст</th><th>Количество</th></tr>
          {{range .Summary.RetainedAgeBuckets}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет событий удержанных объектов.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="custom" class="panel">
    <div class="panel-head">
      <div><h2>Пользовательские метрики</h2><div class="panel-kicker">Счетчики, gauge-метрики и метрики AndroidX JankStats, если они доступны.</div></div>
    </div>
    <details class="fold">
      <summary>Детали метрик</summary>
      <div class="fold-body">
        <div class="triad">
          <div>
            <h3>Счетчики</h3>
            <p class="help-text">Счетчик показывает накопленное количество или объем за сценарий. Для <code>gc.bytes_allocated.delta</code> значение <code>4092288</code> означает примерно 4 МБ новых аллокаций: это не всегда плохо само по себе, но опасно рядом с ростом PSS, GC и подтормаживаниями UI.</p>
            <table><tr><th>Имя</th><th>Значение</th><th>Как читать</th></tr>{{range .Summary.Counters}}<tr><td>{{tip .Name (metricHelp .Name)}}</td><td>{{.Value}}</td><td class="muted">{{metricHelp .Name}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Нет счетчиков.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>Gauge-метрики</h3>
            <p class="help-text">Gauge-метрика показывает уровень во времени: среднее, максимум или последнее значение. Высокое значение плохо не само по себе, а когда совпадает с задержками, очередями, памятью или сетевой активностью.</p>
            <table><tr><th>Имя</th><th>Среднее</th><th>Детали</th><th>Как читать</th></tr>{{range .Summary.Gauges}}<tr><td>{{tip .Name (metricHelp .Name)}}</td><td>{{.Value}}</td><td>{{.Extra}}</td><td class="muted">{{metricHelp .Name}}</td></tr>{{else}}<tr><td colspan="4" class="muted">Нет gauge-метрик.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>JankStats</h3>
            <table><tr><th>Метрика</th><th>Значение</th><th>Детали</th></tr>{{range .Summary.JankStats}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Нет метрик JankStats.</td></tr>{{end}}</table>
          </div>
        </div>
      </div>
    </details>
  </section>

  <section id="context" class="panel">
    <div class="panel-head">
      <div><h2>Контекст прогона</h2><div class="panel-kicker">Когорты помогают честно сравнивать версию приложения, сборку, SDK, устройство, процесс, сеть и рут-доступ.</div></div>
    </div>
    <details class="fold">
      <summary>Детали контекста</summary>
      <div class="fold-body">
        <div class="triad">
          <div><h3>Версии приложения</h3><table>{{range .Summary.AppVersions}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">неизвестно</td><td>0</td></tr>{{end}}</table></div>
          <div><h3>SDK</h3><table>{{range .Summary.SDKs}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">неизвестно</td><td>0</td></tr>{{end}}</table></div>
          <div><h3>Устройства</h3><table>{{range .Summary.Devices}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td class="muted">неизвестно</td><td>0</td></tr>{{end}}</table></div>
        </div>
        <h3>Разбивка по процессам</h3>
        <table><tr><th>Процесс</th><th>Сессии</th></tr>{{range .Summary.Processes}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет метаданных процессов.</td></tr>{{end}}</table>
        <h3>Сэмплы сети</h3>
        <table><tr><th>Сеть</th><th>Сэмплы</th></tr>{{range .Summary.Network}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет событий контекста.</td></tr>{{end}}</table>
        <h3>Объединенные когорты</h3>
        <table><tr><th>Когорта</th><th>События</th></tr>{{range .Summary.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет метаданных когорт.</td></tr>{{end}}</table>
      </div>
    </details>
  </section>

  <section id="analysis" class="panel">
    <div class="panel-head">
      <div><h2>Эвристический итог</h2><div class="panel-kicker">Правила поверх всех собранных сигналов. Используйте это как чеклист ревью, а не как математическое доказательство.</div></div>
    </div>
    <div class="analysis-banner {{severityClass .Analysis.Severity}}">
      <div class="eyebrow">Общий статус</div>
      <div class="analysis-status">{{.Analysis.Status}}</div>
      <div class="muted">{{.Analysis.Summary}}</div>
    </div>
    <h3>Находки</h3>
    <div class="finding-list">
      {{range .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">Нет эвристических находок.</div>{{end}}
    </div>
    <h3>Рекомендации</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>Нет дополнительных рекомендаций.</li>{{end}}
    </ul>
  </section>
</main>
</body>
</html>`

const compareTemplate = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter: сравнение</title>
  <style>` + baseCSS + `</style>
</head>
<body>
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter · сравнение</div>
      <h1>Панель контроля регрессий</h1>
      <div class="subhead">создан {{.GeneratedAt}} · база против кандидата · автономный HTML</div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Контекст сравнения</div>
        <strong class="env-device">{{fallback .Comparison.Baseline.Environment.Title "неизвестная база"}} → {{fallback .Comparison.Candidate.Environment.Title "неизвестный кандидат"}}</strong>
        <div class="env-subtitle">база {{humanDuration .Comparison.Baseline.DurationMS}} · кандидат {{humanDuration .Comparison.Candidate.DurationMS}}</div>
        <div class="env-grid">
          <div class="env-item"><div class="env-label">База</div><div class="env-value">{{fallback .Comparison.Baseline.Environment.Title "неизвестно"}}</div><div class="env-detail">{{fallback .Comparison.Baseline.Environment.Subtitle "контекст недоступен"}}</div></div>
          <div class="env-item"><div class="env-label">Кандидат</div><div class="env-value">{{fallback .Comparison.Candidate.Environment.Title "неизвестно"}}</div><div class="env-detail">{{fallback .Comparison.Candidate.Environment.Subtitle "контекст недоступен"}}</div></div>
          <div class="env-item"><div class="env-label">HTTP p95</div><div class="env-value">{{.Comparison.Baseline.HTTPP95MS}} → {{.Comparison.Candidate.HTTPP95MS}} мс</div><div class="env-detail">ошибки {{.Comparison.Baseline.HTTPFailed}} → {{.Comparison.Candidate.HTTPFailed}}</div></div>
          <div class="env-item"><div class="env-label">UI</div><div class="env-value">{{printf "%.2f" .Comparison.Baseline.UIJankPct}}% → {{printf "%.2f" .Comparison.Candidate.UIJankPct}}%</div><div class="env-detail">FPS {{printf "%.1f" .Comparison.Baseline.UIAvgFPS}} → {{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</div></div>
        </div>
      </div>
      <div class="hero-meta">
        <div class="chip">Логи базы <strong>{{.Comparison.Baseline.LogCount}}</strong></div>
        <div class="chip">Логи кандидата <strong>{{.Comparison.Candidate.LogCount}}</strong></div>
        <div class="chip">Дельты <strong>{{len .Comparison.Deltas}}</strong></div>
      </div>
      <div class="hero-actions"><a class="math-link" href="{{.MathReportHref}}">λ Анализ</a></div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#compare">Сравнение</a>
  <a href="#regressions">Регрессии</a>
  <a href="#changes">Где изменилось</a>
  <a href="#flows">Флоу</a>
  <a href="#drilldown">Детали логов</a>
  <a href="#cohorts">Когорты</a>
  <a href="#analysis">Итог</a>
</nav>
<main>
  <section id="compare" class="panel">
    <div class="panel-head">
      <div>
        <h2>Сводная панель сравнения</h2>
        <div class="panel-kicker">База и кандидат по задержкам, плавности, памяти, трафику, удержанным объектам и составу когорт.</div>
      </div>
      <span class="pill">автономный HTML</span>
    </div>
    {{range .Comparison.Warnings}}<p class="warning">{{.}}</p>{{end}}
    <div class="compare-pair-grid">
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "HTTP p95" "95-й процентиль HTTP-задержки. Рост показывает ухудшение хвоста сетевых запросов."}}</div>
        <div class="compare-values"><div class="compare-value"><span>База</span><strong>{{.Comparison.Baseline.HTTPP95MS}} мс</strong></div><div class="compare-value"><span>Кандидат</span><strong>{{.Comparison.Candidate.HTTPP95MS}} мс</strong></div></div>
        <div class="compare-delta">Запросы {{.Comparison.Baseline.HTTPCount}} → {{.Comparison.Candidate.HTTPCount}}, ошибки {{.Comparison.Baseline.HTTPFailed}} → {{.Comparison.Candidate.HTTPFailed}}</div>
      </div>
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "Подтормаживания UI" "Доля кадров, которые были медленнее целевого времени кадра. Чем выше значение у кандидата, тем заметнее просадка интерфейса."}}</div>
        <div class="compare-values"><div class="compare-value"><span>База</span><strong>{{printf "%.2f" .Comparison.Baseline.UIJankPct}}%</strong></div><div class="compare-value"><span>Кандидат</span><strong>{{printf "%.2f" .Comparison.Candidate.UIJankPct}}%</strong></div></div>
        <div class="compare-delta">Средний FPS {{printf "%.1f" .Comparison.Baseline.UIAvgFPS}} → {{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</div>
      </div>
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "Память" "PSS показывает вклад процесса в RAM, удержанные объекты помогают увидеть возможные утечки."}}</div>
        <div class="compare-values"><div class="compare-value"><span>База</span><strong>{{.Comparison.Baseline.MemoryMaxKB}} KB</strong></div><div class="compare-value"><span>Кандидат</span><strong>{{.Comparison.Candidate.MemoryMaxKB}} KB</strong></div></div>
        <div class="compare-delta">Удержанные объекты {{.Comparison.Baseline.Retained}} → {{.Comparison.Candidate.Retained}}</div>
      </div>
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "Главный поток" "Самая длинная пауза главного потока. При 2 мс ANR-watch такие пики особенно важно смотреть рядом с владельцами работ."}}</div>
        <div class="compare-values"><div class="compare-value"><span>База</span><strong>{{.Comparison.Baseline.StallMaxMS}} мс</strong></div><div class="compare-value"><span>Кандидат</span><strong>{{.Comparison.Candidate.StallMaxMS}} мс</strong></div></div>
        <div class="compare-delta">События пауз {{.Comparison.Baseline.StallCount}} → {{.Comparison.Candidate.StallCount}}</div>
      </div>
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "Флоу и причины" "Сумма агрегированных проблемных окон и спама логами по кортежам контекста флоу. Рост показывает, что кандидат чаще попадает в объяснимые проблемные участки сценария."}}</div>
        <div class="compare-values"><div class="compare-value"><span>Проблемы</span><strong>{{summaryProblems .Comparison.Baseline}} → {{summaryProblems .Comparison.Candidate}}</strong></div><div class="compare-value"><span>Спам логами</span><strong>{{summaryLogSpam .Comparison.Baseline}} → {{summaryLogSpam .Comparison.Candidate}}</strong></div></div>
        <div class="compare-delta">Флоу {{len .Comparison.Baseline.Flows}} → {{len .Comparison.Candidate.Flows}}</div>
      </div>
    </div>
    <h3>Кольцевые индикаторы</h3>
    <div class="ring-row">
      <div class="gauge" style="{{ringStyle .Comparison.Candidate.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIJankPct}}%</strong><span>подтормаживания кандидата</span></div></div></div>
      <div class="gauge" style="{{ringStyle (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}%</strong><span>ошибки кандидата</span></div></div></div>
      <div class="gauge" style="{{ringStyle (fpsScore .Comparison.Candidate.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</strong><span>FPS кандидата</span></div></div></div>
    </div>
  </section>

  <section id="regressions" class="panel">
    <div class="panel-head">
      <div><h2>Матрица регрессий</h2><div class="panel-kicker">Серьезность учитывает доверие и размер выборки. Полосы показывают величину регрессии с ограничением до 100%.</div></div>
    </div>
    {{range deltaGroups .Comparison.Deltas}}
    <div class="category-block">
      <h3>{{.Title}}</h3>
      <p class="help-text">{{.Detail}}</p>
      <table class="compare-table">
        <tr><th>Метрика</th><th>База</th><th>Кандидат</th><th>Изменение</th><th>Регрессия</th><th>Серьезность</th><th>Доверие</th><th>Выборка</th><th>Интервал</th></tr>
        {{range .Items}}
        <tr>
          <td class="metric-name">{{tip (deltaLabel .Name) (deltaHelp .Name)}}</td>
          <td>{{deltaValue .Baseline}}</td>
          <td>{{deltaValue .Candidate}}</td>
          <td>{{deltaChange .Change}}</td>
          <td><div class="delta-track"><i class="{{severityClass .Severity}}" style="{{deltaWidth .RegressionPct}}"></i></div></td>
          <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
          <td>{{confidenceLabel .Confidence}}</td>
          <td>{{.SampleSize}}</td>
          <td>{{deltaInterval .Interval}}</td>
        </tr>
        {{end}}
      </table>
    </div>
    {{end}}
    <h3>Худшие регрессии</h3>
    <table>
      <tr><th>Метрика</th><th>Серьезность</th><th>Регрессия</th><th>Доверие</th><th>Выборка</th></tr>
      {{range problemDeltas .Comparison.Deltas}}
      <tr><td>{{deltaLabel .Name}}</td><td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td><td>{{deltaChange .Change}}</td><td>{{confidenceLabel .Confidence}}</td><td>{{.SampleSize}}</td></tr>
      {{else}}<tr><td colspan="5" class="muted">Регрессий высокой или средней серьезности не найдено.</td></tr>{{end}}
    </table>
  </section>

  <section id="changes" class="panel">
    <div class="panel-head">
      <div><h2>Где изменилось</h2><div class="panel-kicker">Парные таблицы показывают конкретные маршруты, экраны и источники, где кандидат отличается от базы.</div></div>
    </div>
    <details class="fold">
      <summary>Сравнение маршрутов, экранов и источников</summary>
      <div class="fold-body">
        <h3>Маршруты</h3>
        <table>
          <tr><th>Маршрут</th><th>Запросы базы</th><th>Запросы кандидата</th><th>Ошибки базы</th><th>Ошибки кандидата</th><th>p95 базы</th><th>p95 кандидата</th><th>Дельта p95</th><th>Серьезность</th><th>Источник</th></tr>
          {{range routeCompareRows .Comparison.Baseline .Comparison.Candidate}}
          <tr>
            <td><code>{{.Route}}</code></td>
            <td>{{.BaselineCount}}</td><td>{{.CandidateCount}}</td>
            <td>{{.BaselineFailures}}</td><td>{{.CandidateFailures}}</td>
            <td>{{.BaselineP95MS}} мс</td><td>{{.CandidateP95MS}} мс</td>
            <td>{{signedMS .DeltaP95MS}}</td>
            <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
            <td><code>{{fallback .CandidateOwner .BaselineOwner}}</code></td>
          </tr>
          {{else}}<tr><td colspan="10" class="muted">Нет HTTP-событий для сравнения.</td></tr>{{end}}
        </table>
        <h3>Экраны</h3>
        <table>
          <tr><th>Экран</th><th>Кадры базы</th><th>Кадры кандидата</th><th>Подтормаживания базы</th><th>Подтормаживания кандидата</th><th>Дельта</th><th>FPS базы</th><th>FPS кандидата</th><th>Дельта FPS</th><th>Серьезность</th></tr>
          {{range screenCompareRows .Comparison.Baseline .Comparison.Candidate}}
          <tr>
            <td><code>{{.Screen}}</code></td>
            <td>{{.BaselineFrames}}</td><td>{{.CandidateFrames}}</td>
            <td>{{printf "%.2f" .BaselineJankPct}}%</td><td>{{printf "%.2f" .CandidateJankPct}}%</td>
            <td>{{signedFloat .DeltaJankPct "п.п."}}</td>
            <td>{{printf "%.1f" .BaselineAvgFPS}}</td><td>{{printf "%.1f" .CandidateAvgFPS}}</td>
            <td>{{signedFloat .DeltaFPS "FPS"}}</td>
            <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
          </tr>
          {{else}}<tr><td colspan="10" class="muted">Нет UI-окон для сравнения.</td></tr>{{end}}
        </table>
        <h3>Источники</h3>
        <table>
          <tr><th>Источник / класс</th><th>Тип</th><th>Количество базы</th><th>Количество кандидата</th><th>Макс. базы</th><th>Макс. кандидата</th><th>Дельта макс.</th><th>Итого базы</th><th>Итого кандидата</th><th>Серьезность</th></tr>
          {{range ownerCompareRows .Comparison.Baseline .Comparison.Candidate}}
          <tr>
            <td><code>{{.Owner}}</code></td><td>{{ownerKind .Kind}}</td>
            <td>{{.BaselineCount}}</td><td>{{.CandidateCount}}</td>
            <td>{{.BaselineMaxMS}} мс</td><td>{{.CandidateMaxMS}} мс</td>
            <td>{{signedMS .DeltaMaxMS}}</td>
            <td>{{.BaselineTotalMS}} мс</td><td>{{.CandidateTotalMS}} мс</td>
            <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
          </tr>
          {{else}}<tr><td colspan="10" class="muted">Атрибуция источников пока недоступна.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="flows" class="panel">
    <div class="panel-head">
      <div><h2>Сравнение флоу и причин</h2><div class="panel-kicker">Экран, флоу, шаг и источник сопоставлены между базой и кандидатом по проблемным окнам, спаму логами, HTTP, UI и паузам.</div></div>
    </div>
    <details class="fold" open>
      <summary>Детали флоу</summary>
      <div class="fold-body">
        <table>
          <tr><th>Контекст</th><th>Проблемы базы</th><th>Проблемы кандидата</th><th>Δ проблем</th><th>Спам базы</th><th>Спам кандидата</th><th>Δ спама</th><th>HTTP p95 база</th><th>HTTP p95 кандидат</th><th>Макс. пауза база</th><th>Макс. пауза кандидат</th><th>UI база</th><th>UI кандидат</th><th>Серьезность</th></tr>
          {{range flowCompareRows .Comparison.Baseline .Comparison.Candidate}}
          <tr>
            <td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td>
            <td>{{.BaselineProblems}}</td><td>{{.CandidateProblems}}</td><td>{{.DeltaProblems}}</td>
            <td>{{.BaselineLogSpam}}</td><td>{{.CandidateLogSpam}}</td><td>{{.DeltaLogSpam}}</td>
            <td>{{.BaselineHTTPP95MS}} мс</td><td>{{.CandidateHTTPP95MS}} мс</td>
            <td>{{.BaselineStallMaxMS}} мс</td><td>{{.CandidateStallMaxMS}} мс</td>
            <td>{{printf "%.2f" .BaselineJankPct}}%</td><td>{{printf "%.2f" .CandidateJankPct}}%</td>
            <td class="{{severityClass .Severity}}">{{severityLabel .Severity}}</td>
          </tr>
          {{else}}<tr><td colspan="14" class="muted">Нет событий контекста флоу для сравнения.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="drilldown" class="panel">
    <div class="panel-head">
      <div><h2>Детали по каждому логу</h2><div class="panel-kicker">Откройте любой исходный лог, чтобы увидеть его сеть, UI, память, метрики и профиль влияния.</div></div>
    </div>
    <h3>Логи базы</h3>
    {{range .BaselineLogs}}
    <details class="log-card" id="{{.Anchor}}">
      <summary>
        <div><strong class="mono-block">{{.Name}}</strong><div class="muted">{{.Summary.EventCount}} событий · {{humanDuration .Summary.DurationMS}} · {{.Summary.LogCount}} логов</div></div>
        <div class="summary-metrics"><span class="pill">HTTP p95 {{.Summary.HTTPP95MS}} мс</span><span class="pill">Подтормаживания {{printf "%.2f" .Summary.UIJankPct}}%</span><span class="pill">FPS {{printf "%.1f" .Summary.UIAvgFPS}}</span></div>
      </summary>
      <div class="log-body">
        <div class="detail-grid">
          <div><h3>Маршруты</h3><table><tr><th>Маршрут</th><th>Количество</th><th>Ошибки</th><th>p95</th><th>Макс.</th></tr>{{range .Summary.Routes}}<tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P95MS}} мс</td><td>{{.MaxMS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет HTTP-событий.</td></tr>{{end}}</table></div>
          <div><h3>Экраны</h3><table><tr><th>Экран</th><th>Кадры</th><th>Подтормаживания</th><th>FPS</th><th>p95</th></tr>{{range .Summary.Screens}}<tr><td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{printf "%.2f" .JankRatePct}}%</td><td>{{printf "%.1f" .AvgFPS}}</td><td>{{.P95MS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет событий UI-окон.</td></tr>{{end}}</table></div>
          <div><h3>Источники</h3><table><tr><th>Источник</th><th>Тип</th><th>Количество</th><th>Макс.</th></tr>{{range .Summary.Owners}}<tr><td><code>{{.Owner}}</code></td><td>{{ownerKind .Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} мс</td></tr>{{else}}<tr><td colspan="4" class="muted">Нет источников.</td></tr>{{end}}</table></div>
          <div><h3>Память и метрики</h3><table><tr><th>Сигнал</th><th>Значение</th><th>Детали</th></tr><tr><td>max_pss_kb</td><td>{{.Summary.MemoryMaxKB}}</td><td>удержано={{.Summary.Retained}}</td></tr>{{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{end}}</table></div>
          <div><h3>Флоу и причины</h3><table><tr><th>Экран / флоу / шаг / источник</th><th>Проблемы</th><th>Спам логами</th><th>HTTP p95</th><th>Макс. пауза</th></tr>{{range .Summary.Flows}}<tr><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td><td>{{.ProblemCount}}</td><td>{{.LogSpam}}</td><td>{{.HTTPP95MS}} мс</td><td>{{.StallMaxMS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет флоу.</td></tr>{{end}}</table></div>
          <div><h3>Проблемные окна</h3><table><tr><th>Причина</th><th>Количество</th><th>Макс.</th><th>Контекст</th></tr>{{range .Summary.ProblemWindows}}<tr><td>{{problemKind .Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} мс</td><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td></tr>{{else}}<tr><td colspan="4" class="muted">Нет проблемных окон.</td></tr>{{end}}</table></div>
        </div>
      </div>
    </details>
    {{else}}<div class="muted">Детали логов базы не встроены.</div>{{end}}
    <h3>Логи кандидата</h3>
    {{range .CandidateLogs}}
    <details class="log-card" id="{{.Anchor}}">
      <summary>
        <div><strong class="mono-block">{{.Name}}</strong><div class="muted">{{.Summary.EventCount}} событий · {{humanDuration .Summary.DurationMS}} · {{.Summary.LogCount}} логов</div></div>
        <div class="summary-metrics"><span class="pill">HTTP p95 {{.Summary.HTTPP95MS}} мс</span><span class="pill">Подтормаживания {{printf "%.2f" .Summary.UIJankPct}}%</span><span class="pill">FPS {{printf "%.1f" .Summary.UIAvgFPS}}</span></div>
      </summary>
      <div class="log-body">
        <div class="detail-grid">
          <div><h3>Маршруты</h3><table><tr><th>Маршрут</th><th>Количество</th><th>Ошибки</th><th>p95</th><th>Макс.</th></tr>{{range .Summary.Routes}}<tr><td><code>{{.Route}}</code></td><td>{{.Count}}</td><td>{{.Failures}}</td><td>{{.P95MS}} мс</td><td>{{.MaxMS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет HTTP-событий.</td></tr>{{end}}</table></div>
          <div><h3>Экраны</h3><table><tr><th>Экран</th><th>Кадры</th><th>Подтормаживания</th><th>FPS</th><th>p95</th></tr>{{range .Summary.Screens}}<tr><td><code>{{.Screen}}</code></td><td>{{.Frames}}</td><td>{{printf "%.2f" .JankRatePct}}%</td><td>{{printf "%.1f" .AvgFPS}}</td><td>{{.P95MS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет событий UI-окон.</td></tr>{{end}}</table></div>
          <div><h3>Источники</h3><table><tr><th>Источник</th><th>Тип</th><th>Количество</th><th>Макс.</th></tr>{{range .Summary.Owners}}<tr><td><code>{{.Owner}}</code></td><td>{{ownerKind .Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} мс</td></tr>{{else}}<tr><td colspan="4" class="muted">Нет источников.</td></tr>{{end}}</table></div>
          <div><h3>Память и метрики</h3><table><tr><th>Сигнал</th><th>Значение</th><th>Детали</th></tr><tr><td>max_pss_kb</td><td>{{.Summary.MemoryMaxKB}}</td><td>удержано={{.Summary.Retained}}</td></tr>{{range .Summary.Gauges}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td><td>{{.Extra}}</td></tr>{{end}}</table></div>
          <div><h3>Флоу и причины</h3><table><tr><th>Экран / флоу / шаг / источник</th><th>Проблемы</th><th>Спам логами</th><th>HTTP p95</th><th>Макс. пауза</th></tr>{{range .Summary.Flows}}<tr><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td><td>{{.ProblemCount}}</td><td>{{.LogSpam}}</td><td>{{.HTTPP95MS}} мс</td><td>{{.StallMaxMS}} мс</td></tr>{{else}}<tr><td colspan="5" class="muted">Нет флоу.</td></tr>{{end}}</table></div>
          <div><h3>Проблемные окна</h3><table><tr><th>Причина</th><th>Количество</th><th>Макс.</th><th>Контекст</th></tr>{{range .Summary.ProblemWindows}}<tr><td>{{problemKind .Kind}}</td><td>{{.Count}}</td><td>{{.MaxMS}} мс</td><td><code>{{flowKeyLabel .Screen .Flow .Step .Owner}}</code></td></tr>{{else}}<tr><td colspan="4" class="muted">Нет проблемных окон.</td></tr>{{end}}</table></div>
        </div>
      </div>
    </details>
    {{else}}<div class="muted">Детали логов кандидата не встроены.</div>{{end}}
  </section>

  <section id="cohorts" class="panel">
    <div class="panel-head">
      <div><h2>Разбивка по когортам</h2><div class="panel-kicker">Используйте это, чтобы проверить честность сравнения по версии приложения, SDK, устройству, процессу, сети и рут-доступу.</div></div>
    </div>
    <details class="fold">
      <summary>Детали когорт</summary>
      <div class="fold-body">
        <div class="split">
          <div>
            <h3>База</h3>
            <table><tr><th>Когорта</th><th>События</th></tr>{{range .Comparison.Baseline.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет метаданных когорт.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>Кандидат</h3>
            <table><tr><th>Когорта</th><th>События</th></tr>{{range .Comparison.Candidate.Cohorts}}<tr><td><code>{{.Name}}</code></td><td>{{.Value}}</td></tr>{{else}}<tr><td colspan="2" class="muted">Нет метаданных когорт.</td></tr>{{end}}</table>
          </div>
        </div>
        <h3>Контекст устройств</h3>
        <div class="split">
          <div>
            <h3>База</h3>
            <table><tr><th>Сигнал</th><th>Значение</th><th>Детали</th></tr>{{range .Comparison.Baseline.Environment.Items}}<tr><td>{{.Label}}</td><td>{{.Value}}</td><td>{{.Detail}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Контекст устройства базы недоступен.</td></tr>{{end}}</table>
          </div>
          <div>
            <h3>Кандидат</h3>
            <table><tr><th>Сигнал</th><th>Значение</th><th>Детали</th></tr>{{range .Comparison.Candidate.Environment.Items}}<tr><td>{{.Label}}</td><td>{{.Value}}</td><td>{{.Detail}}</td></tr>{{else}}<tr><td colspan="3" class="muted">Контекст устройства кандидата недоступен.</td></tr>{{end}}</table>
          </div>
        </div>
        <h3>Состав процессов</h3>
        <table>
          <tr><th>Процесс базы</th><th>Сессии</th><th>Процесс кандидата</th><th>Сессии</th></tr>
          <tr>
            <td>{{range .Comparison.Baseline.Processes}}<div><code>{{.Name}}</code></div>{{else}}<span class="muted">неизвестно</span>{{end}}</td>
            <td>{{range .Comparison.Baseline.Processes}}<div>{{.Value}}</div>{{else}}<span class="muted">0</span>{{end}}</td>
            <td>{{range .Comparison.Candidate.Processes}}<div><code>{{.Name}}</code></div>{{else}}<span class="muted">неизвестно</span>{{end}}</td>
            <td>{{range .Comparison.Candidate.Processes}}<div>{{.Value}}</div>{{else}}<span class="muted">0</span>{{end}}</td>
          </tr>
        </table>
      </div>
    </details>
  </section>

  <section id="analysis" class="panel">
    <div class="panel-head">
      <div><h2>Эвристический итог</h2><div class="panel-kicker">Правила поверх всех дельт сравнения и предупреждений по когортам. Используйте это как чеклист ревью, а не как математическое доказательство.</div></div>
    </div>
    <div class="analysis-banner {{severityClass .Analysis.Severity}}">
      <div class="eyebrow">Общий статус</div>
      <div class="analysis-status">{{.Analysis.Status}}</div>
      <div class="muted">{{.Analysis.Summary}}</div>
    </div>
    <h3>Находки</h3>
    <div class="finding-list">
      {{range .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">Нет эвристических находок.</div>{{end}}
    </div>
    <h3>Рекомендации</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>Нет дополнительных рекомендаций.</li>{{end}}
    </ul>
  </section>
</main>
</body>
</html>`
