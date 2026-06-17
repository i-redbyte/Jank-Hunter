package report

const baseCSS = `
:root {
  color-scheme: dark;
  --bg: #070a12;
  --bg-2: #0b1020;
  --panel: rgba(12, 18, 34, 0.90);
  --panel-strong: rgba(17, 28, 52, 0.98);
  --line: rgba(126, 247, 255, 0.23);
  --line-strong: rgba(126, 247, 255, 0.42);
  --ink: #eef8ff;
  --muted: #a4b7c9;
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
body.presentation-page {
  --panel: rgba(9, 14, 26, 0.94);
  --panel-strong: rgba(13, 22, 42, 0.99);
}
body.presentation-page .hero {
  min-height: 70vh;
  display: grid;
  align-items: center;
}
body.presentation-page .nav {
  position: sticky;
  top: 0;
}
body.presentation-page main {
  max-width: 1600px;
}
body.presentation-page .panel,
body.presentation-page .fold {
  scroll-margin-top: 82px;
}
body.presentation-page .panel h2,
body.presentation-page .fold summary {
  font-size: 24px;
}
body.presentation-page .metric .value {
  font-size: 34px;
}
body.presentation-page .problem-score,
body.presentation-page .score-band {
  font-size: 14px;
}
@media print {
  body.presentation-page {
    color: #08111f;
    background: #fff;
  }
  body.presentation-page::before,
  body.presentation-page::after,
  body.presentation-page .nav,
  body.presentation-page .hero-actions,
  body.presentation-page .jh-tooltip {
    display: none !important;
  }
  body.presentation-page .hero,
  body.presentation-page .metric,
  body.presentation-page .panel,
  body.presentation-page .fold,
  body.presentation-page .env-card {
    break-inside: avoid;
    box-shadow: none;
    background: #fff;
    color: #08111f;
    border-color: #ccd6e3;
  }
  body.presentation-page a,
  body.presentation-page code,
  body.presentation-page .muted,
  body.presentation-page .hint {
    color: #22324a;
  }
  body.presentation-page .fold-body,
  body.presentation-page .log-body {
    max-height: none;
    overflow: visible;
  }
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
.report-hero-grid {
  display: grid;
  gap: 24px;
  max-width: 1280px;
  margin: 0 auto;
}
.report-hero-title {
  max-width: 920px;
}
.report-hero-lower {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 390px);
  gap: 24px;
  align-items: stretch;
}
.report-hero-context .env-card {
  min-height: 100%;
}
.report-hero-control {
  display: grid;
  grid-template-rows: auto auto;
  align-content: start;
  gap: 12px;
}
.report-hero-control .hero-meta {
  grid-template-columns: 1fr;
}
.report-hero-control .hero-actions {
  display: grid;
  grid-template-columns: 1fr;
  align-content: start;
  gap: 12px;
  margin-top: 0;
}
.report-hero-control .math-link {
  min-height: 62px;
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
  max-width: 1440px;
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
.metric .hint {
  margin-top: 4px;
  max-width: 100%;
  color: var(--muted);
  font-size: 12px;
  overflow-wrap: break-word;
  word-break: normal;
}
.influence-grid {
  align-items: stretch;
}
.influence-tile {
  height: 172px;
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  gap: 4px;
}
.influence-tile .value {
  margin-top: 2px;
}
.influence-tile-body {
  min-height: 0;
  overflow-x: hidden;
  overflow-y: auto;
  padding-right: 6px;
  overscroll-behavior: contain;
  scrollbar-width: thin;
  scrollbar-color: rgba(111,247,255,0.34) rgba(255,255,255,0.04);
}
.influence-tile-body::-webkit-scrollbar {
  width: 7px;
  height: 7px;
}
.influence-tile-body::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: rgba(111,247,255,0.34);
}
.influence-tile-body::-webkit-scrollbar-track {
  background: rgba(255,255,255,0.04);
}
.influence-tile-body:focus {
  outline: 1px solid rgba(111,247,255,0.42);
  outline-offset: 2px;
  border-radius: 6px;
}
.influence-tile-body code {
  max-width: 100%;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: normal;
}
.influence-tile-reasons {
  line-height: 1.35;
  overflow-wrap: break-word;
  word-break: normal;
}
.code-registry {
  display: grid;
  gap: 14px;
}
.registry-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(255,255,255,0.035);
}
.registry-toolbar input,
.registry-toolbar select {
  min-height: 38px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: rgba(5,9,18,0.64);
  color: var(--ink);
  padding: 8px 10px;
  font: inherit;
}
.registry-toolbar input {
  flex: 1 1 280px;
  min-width: 220px;
}
.registry-toolbar select {
  flex: 0 1 180px;
}
.registry-counter {
  margin-left: auto;
  color: var(--muted);
  font-size: 12px;
  font-weight: 800;
}
.registry-insights {
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  align-items: center;
  margin: 8px 0 12px;
}
.registry-insights-label {
  color: var(--muted);
  font-size: 11px;
  font-weight: 850;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.registry-chip {
  appearance: none;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 4px 9px;
  border: 1px solid rgba(126,247,255,0.18);
  border-radius: 999px;
  color: var(--ink);
  background: rgba(111,247,255,0.045);
  font: inherit;
  font-size: 11px;
  font-weight: 850;
  cursor: pointer;
}
.registry-chip strong {
  color: var(--cyan);
}
.registry-chip.sev-high { border-color: rgba(255,91,124,0.42); color: var(--bad); }
.registry-chip.sev-medium { border-color: rgba(255,209,102,0.42); color: var(--warn); }
.registry-chip.sev-ok { border-color: rgba(98,255,168,0.32); color: var(--ok); }
.registry-chip.is-active,
.registry-chip:hover,
.registry-chip:focus-visible {
  outline: none;
  border-color: rgba(126,247,255,0.56);
  box-shadow: 0 0 0 3px rgba(111,247,255,0.10);
}
.code-problem-table:not(.leak-table) {
  min-width: 1080px;
  table-layout: fixed;
}
.leak-table {
  min-width: 1900px;
}
.code-problem-table td,
.leak-table td {
  overflow: hidden;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: normal;
}
.code-problem-table td code,
.leak-table td code {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  overflow-wrap: anywhere;
  word-break: break-word;
  white-space: normal;
}
.leak-object-kind {
  color: var(--cyan);
  font-size: 12px;
  font-weight: 850;
}
.leak-holder-quality {
  margin-top: 5px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.35;
}
.leak-chain-summary {
  margin-top: 8px;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.45;
}
.leak-chain-actions {
  display: grid;
  gap: 6px;
  margin-top: 8px;
}
.leak-chain-actions strong {
  color: var(--cyan);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.leak-chain-actions span {
  display: block;
  padding-left: 10px;
  border-left: 2px solid rgba(126,247,255,0.26);
  color: var(--ink);
  font-size: 12px;
  line-height: 1.45;
}
.leak-limitations {
  margin-top: 10px;
  padding: 10px 12px;
  border: 1px solid rgba(255,209,102,0.28);
  border-radius: 8px;
  color: var(--muted);
  background: rgba(255,209,102,0.055);
}
.problem-drilldown {
  display: grid;
  gap: 8px;
  margin-top: 10px;
}
.problem-drill {
  display: grid;
  gap: 4px;
  padding: 8px 10px;
  border-left: 2px solid rgba(126,247,255,0.32);
  background: rgba(126,247,255,0.045);
}
.problem-drill strong {
  color: var(--cyan);
  font-size: 12px;
}
.problem-drill span {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.35;
}
.code-problem-table th {
  vertical-align: middle;
}
.code-problem-table:not(.leak-table) th:nth-child(1),
.code-problem-table:not(.leak-table) td:nth-child(1) { width: 130px; }
.code-problem-table:not(.leak-table) th:nth-child(2),
.code-problem-table:not(.leak-table) td:nth-child(2) { width: 280px; }
.code-problem-table:not(.leak-table) th:nth-child(3),
.code-problem-table:not(.leak-table) td:nth-child(3) { width: 230px; }
.code-problem-table:not(.leak-table) th:nth-child(4),
.code-problem-table:not(.leak-table) td:nth-child(4) { width: auto; }
.code-problem-table:not(.leak-table) th:nth-child(5),
.code-problem-table:not(.leak-table) td:nth-child(5) { width: auto; }
.code-problem-table:not(.leak-table) td:nth-child(3),
.code-problem-table:not(.leak-table) td:nth-child(4),
.code-problem-table:not(.leak-table) td:nth-child(5) {
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: normal;
}
.leak-table th:nth-child(7),
.leak-table td:nth-child(7) { min-width: 320px; }
.leak-table th:nth-child(8),
.leak-table td:nth-child(8) { min-width: 520px; }
.leak-table th:nth-child(9),
.leak-table td:nth-child(9) { min-width: 420px; }
.leak-table th:nth-child(10),
.leak-table td:nth-child(10) { min-width: 520px; }
.leak-table th:nth-child(11),
.leak-table td:nth-child(11) { min-width: 380px; }
.leak-dominator {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  max-width: 100%;
}
.leak-dominator span {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 7px;
  border: 1px solid rgba(126,247,255,0.16);
  border-radius: 999px;
  background: rgba(111,247,255,0.05);
  color: var(--ink);
  font-size: 11px;
  font-weight: 800;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: normal;
}
.leak-dominator span + span::before {
  content: "→";
  margin-right: 6px;
  color: var(--cyan);
}
.code-problem-table th button {
  appearance: none;
  border: 0;
  background: transparent;
  color: inherit;
  padding: 0;
  font: inherit;
  text-transform: inherit;
  letter-spacing: inherit;
  cursor: pointer;
}
.code-problem-table th button::after {
  content: "↕";
  margin-left: 5px;
  color: rgba(111,247,255,0.58);
}
.code-problem-table th button.active.asc::after { content: "↑"; }
.code-problem-table th button.active.desc::after { content: "↓"; }
.problem-score {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 56px;
  padding: 5px 8px;
  border: 1px solid rgba(126,247,255,0.28);
  border-radius: 999px;
  font-weight: 900;
}
.problem-score.sev-high { border-color: rgba(255,91,124,0.52); color: var(--bad); }
.problem-score.sev-medium { border-color: rgba(255,209,102,0.52); color: var(--warn); }
.problem-score.sev-ok { border-color: rgba(98,255,168,0.42); color: var(--ok); }
.problem-tags,
.problem-context,
.problem-signals {
  display: flex;
  align-items: flex-start;
  flex-wrap: wrap;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
}
.problem-tags {
  max-width: none;
}
.problem-chip {
  display: inline-flex;
  width: fit-content;
  max-width: 100%;
  padding: 4px 7px;
  border: 1px solid rgba(126,247,255,0.2);
  border-radius: 999px;
  background: rgba(111,247,255,0.055);
  color: var(--ink);
  font-size: 11px;
  font-weight: 850;
  white-space: nowrap;
}
.problem-chip.sev-high { border-color: rgba(255,91,124,0.42); color: var(--bad); }
.problem-chip.sev-medium { border-color: rgba(255,209,102,0.42); color: var(--warn); }
.problem-location {
  display: grid;
  gap: 5px;
  min-width: 0;
  max-width: 100%;
}
.problem-location .method {
  color: var(--muted);
  font-size: 12px;
}
.code-problem-details {
  border: 1px solid rgba(126,247,255,0.16);
  border-radius: 8px;
  background: rgba(255,255,255,0.026);
}
.code-problem-details summary {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 42px;
  padding: 9px 11px;
  color: var(--ink);
  cursor: pointer;
}
.code-problem-summary-main {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.code-problem-summary-main strong {
  color: var(--ink);
  font-weight: 850;
}
.code-problem-summary-main em {
  display: -webkit-box;
  overflow: hidden;
  color: var(--muted);
  font-size: 12px;
  font-style: normal;
  font-weight: 650;
  line-height: 1.35;
  overflow-wrap: anywhere;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}
.code-problem-details summary small {
  flex: 0 0 auto;
  color: var(--muted);
  font-size: 11px;
  font-weight: 800;
  text-align: right;
}
.code-problem-details[open] summary {
  border-bottom: 1px solid rgba(126,247,255,0.12);
  color: var(--cyan);
}
.code-problem-details[open] .code-problem-summary-main strong {
  color: var(--cyan);
}
.code-problem-detail-body {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding: 11px;
}
.code-problem-detail-block {
  min-width: 0;
  padding: 9px 10px;
  border: 1px solid rgba(126,247,255,0.10);
  border-radius: 8px;
  background: rgba(6,12,26,0.42);
}
.code-problem-detail-block.span-all {
  grid-column: 1 / -1;
}
.code-problem-detail-block strong {
  display: block;
  margin-bottom: 5px;
  color: var(--cyan);
  font-size: 11px;
  font-weight: 900;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.code-problem-detail-block p {
  margin: 0;
  color: var(--muted);
  line-height: 1.45;
}
.problem-signal {
  min-width: 260px;
  max-width: 360px;
  max-height: 98px;
  overflow: auto;
  padding: 7px 8px;
  border: 1px solid rgba(126,247,255,0.12);
  border-radius: 8px;
  background: rgba(255,255,255,0.028);
  scrollbar-width: thin;
}
.problem-signal strong {
  display: block;
  margin-bottom: 2px;
}
.problem-signal small {
  display: block;
  color: var(--muted);
  line-height: 1.35;
  white-space: normal;
  overflow-wrap: normal;
  word-break: normal;
}
.problem-drill,
.problem-drill strong,
.problem-drill span {
  min-width: 0;
  overflow-wrap: anywhere;
  word-break: normal;
}
.problem-empty {
  display: none;
  color: var(--muted);
}
.code-registry.no-results .problem-empty {
  display: block;
}
.panel, .log-card {
  margin: 18px 0;
  padding: 18px;
  overflow-x: visible;
  overflow-y: visible;
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
.metric .explain {
  color: var(--muted);
}
.jh-tooltip {
  position: fixed;
  z-index: 2147483647;
  width: max-content;
  min-width: min(220px, calc(100vw - 24px));
  max-width: min(520px, calc(100vw - 24px));
  max-height: min(42vh, 360px);
  overflow: auto;
  padding: 10px 12px;
  border: 1px solid rgba(111,247,255,0.45);
  border-radius: 8px;
  color: var(--ink);
  background:
    linear-gradient(135deg, rgba(111,247,255,0.14), rgba(255,79,216,0.11)),
    rgba(7,10,18,0.98);
  box-shadow: 0 18px 54px rgba(0,0,0,0.48), 0 0 22px rgba(111,247,255,0.18);
  font-size: 12px;
  font-weight: 650;
  line-height: 1.45;
  white-space: normal;
  overflow-wrap: break-word;
  word-break: normal;
  pointer-events: none;
  opacity: 0;
  transform: translate3d(0, 4px, 0);
  transition: opacity 120ms ease, transform 120ms ease;
}
.jh-tooltip.is-visible {
  opacity: 1;
  transform: translate3d(0, 0, 0);
}
.split { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.triad { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; }
.split > *, .triad > *, .detail-grid > * { min-width: 0; }
.ring-row { display: flex; gap: 18px; flex-wrap: wrap; align-items: start; }
.gauge-card {
  width: 170px;
  height: 198px;
  display: grid;
  grid-template-rows: 156px 34px;
  justify-items: center;
  align-items: start;
  gap: 8px;
}
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
.gauge-core span {
  display: block;
  margin-top: 5px;
  color: var(--muted);
  font-size: 10px;
  font-weight: 850;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.gauge-label {
  width: 156px;
  height: 34px;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  color: var(--muted);
  font-size: 12px;
  font-weight: 850;
  line-height: 1.25;
  text-align: center;
  overflow-wrap: normal;
  word-break: normal;
  hyphens: none;
  text-wrap: balance;
}
.table-scroll {
  width: 100%;
  max-width: 100%;
  margin: 8px 0 14px;
  overflow-x: auto;
  overflow-y: hidden;
  border: 1px solid rgba(126,247,255,0.10);
  border-radius: 8px;
  background: rgba(5,9,18,0.12);
  -webkit-overflow-scrolling: touch;
  scrollbar-width: thin;
  scrollbar-color: rgba(111,247,255,0.36) rgba(255,255,255,0.05);
}
.table-scroll::-webkit-scrollbar {
  height: 10px;
}
.table-scroll::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: rgba(111,247,255,0.36);
}
.table-scroll::-webkit-scrollbar-track {
  border-radius: 999px;
  background: rgba(255,255,255,0.05);
}
.table-scroll::after {
  content: "Прокрутите таблицу по горизонтали, если не видны все колонки";
  display: none;
  padding: 6px 10px 8px;
  color: var(--muted);
  font-size: 11px;
  border-top: 1px solid rgba(126,247,255,0.08);
}
.table-scroll.is-scrollable::after {
  display: block;
}
.table-scroll table {
  width: max-content;
  min-width: 100%;
  margin: 0;
}
.table-cell-clip {
  position: relative;
  display: block;
  max-width: min(680px, 72vw);
  max-height: 92px;
  overflow: hidden;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: normal;
}
.table-cell-clip code {
  overflow-wrap: anywhere;
  word-break: break-word;
  white-space: normal;
}
.table-cell-clip::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 22px;
  pointer-events: none;
  background: linear-gradient(180deg, rgba(12,18,31,0), rgba(12,18,31,0.96));
}
.table-cell-clip.is-expanded {
  max-width: min(980px, 82vw);
  max-height: none;
  overflow: visible;
}
.table-cell-clip.is-expanded::after {
  display: none;
}
.cell-toggle {
  appearance: none;
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  margin-top: 7px;
  padding: 3px 8px;
  border: 1px solid rgba(126,247,255,0.22);
  border-radius: 999px;
  color: var(--cyan);
  background: rgba(111,247,255,0.055);
  font: inherit;
  font-size: 11px;
  font-weight: 850;
  cursor: pointer;
}
.cell-toggle:hover,
.cell-toggle:focus-visible {
  border-color: rgba(126,247,255,0.5);
  outline: none;
  box-shadow: 0 0 0 3px rgba(111,247,255,0.10);
}
table {
  width: 100%;
  min-width: 840px;
  max-width: none;
  border-collapse: separate;
  border-spacing: 0;
  table-layout: auto;
  overflow: visible;
}
th, td {
  min-width: 92px;
  max-width: none;
  padding: 11px 13px;
  border-bottom: 1px solid rgba(126,247,255,0.12);
  text-align: left;
  vertical-align: top;
  overflow: hidden;
  text-overflow: clip;
  overflow-wrap: normal;
  word-break: keep-all;
  hyphens: none;
  white-space: nowrap;
  line-height: 1.45;
}
th {
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  white-space: nowrap;
}
td:first-child, th:first-child {
  min-width: 160px;
}
td:last-child {
  white-space: nowrap;
  overflow-wrap: normal;
}
tr:hover td { background: rgba(111,247,255,0.035); }
.muted { color: var(--muted); }
code {
  display: inline-block;
  max-width: 100%;
  color: #d8fcff;
  font-family: "JetBrains Mono", "SFMono-Regular", Consolas, monospace;
  font-size: 12px;
  line-height: 1.35;
  vertical-align: bottom;
  overflow: hidden;
  text-overflow: ellipsis;
  overflow-wrap: normal;
  word-break: normal;
  white-space: nowrap;
}
td code,
th code {
  display: inline-block;
  max-width: 100%;
  vertical-align: bottom;
  overflow: hidden;
  text-overflow: ellipsis;
  overflow-wrap: normal;
  word-break: keep-all;
  white-space: nowrap;
}
.code-problem-table td code,
.leak-table td code {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  overflow-wrap: anywhere;
  word-break: break-word;
  white-space: normal;
}
.muted,
.help-text,
.panel-kicker,
.finding,
.analysis-banner,
.section-overview-summary,
.heuristic-card,
.table-metric,
.reference-block {
  overflow-wrap: break-word;
  word-break: normal;
  hyphens: none;
}
.cell-stack {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.score-note {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  padding: 3px 8px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: rgba(255,255,255,0.035);
  font-size: 12px;
  font-weight: 850;
  white-space: nowrap;
}
.score-note.sev-high { color: var(--bad); border-color: rgba(255,91,124,0.46); }
.score-note.sev-medium { color: var(--warn); border-color: rgba(255,209,102,0.46); }
.score-note.sev-ok { color: var(--ok); border-color: rgba(98,255,168,0.36); }
.report-guide {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 10px;
  margin: 14px 0 2px;
}
.guide-card {
  min-width: 0;
  padding: 12px;
  border: 1px solid rgba(126,247,255,0.14);
  border-radius: 8px;
  background: rgba(255,255,255,0.03);
}
.guide-card strong {
  display: block;
  margin-bottom: 5px;
  color: var(--cyan);
}
.score-guide {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 10px;
  margin: 10px 0 14px;
}
.score-guide-card {
  padding: 11px 12px;
  border: 1px solid rgba(126,247,255,0.14);
  border-radius: 8px;
  background: rgba(255,255,255,0.032);
}
.score-guide-card strong {
  display: block;
  margin-bottom: 8px;
  color: var(--cyan);
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}
.score-guide-card p {
  margin: 8px 0 0;
  color: var(--muted);
  font-size: 12px;
  line-height: 1.45;
}
.score-band {
  display: inline-flex;
  align-items: center;
  min-height: 24px;
  margin: 0 6px 6px 0;
  padding: 3px 8px;
  border-radius: 999px;
  border: 1px solid var(--line);
  font-size: 11px;
  font-weight: 850;
}
.score-band.sev-high { border-color: rgba(255,91,124,0.42); color: var(--bad); }
.score-band.sev-medium { border-color: rgba(255,209,102,0.42); color: var(--warn); }
.score-band.sev-ok { border-color: rgba(98,255,168,0.36); color: var(--ok); }
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
  overflow-x: hidden;
  overflow-y: auto;
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
.fold-body .split,
.fold-body .triad,
.log-body .split,
.log-body .triad {
  grid-template-columns: 1fr;
}
.panel .split:has(table),
.panel .triad:has(table) {
  grid-template-columns: 1fr;
}
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
  word-break: keep-all;
  white-space: nowrap;
}
.compare-table code,
#changes table code,
#cohorts table code {
  overflow-wrap: normal;
  word-break: keep-all;
  white-space: nowrap;
}
.table-stack {
  display: grid;
  gap: 5px;
}
.table-stack span {
  display: block;
}
.table-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px;
  min-width: 0;
}
.table-metric {
  padding: 5px 7px;
  border: 1px solid rgba(126,247,255,0.12);
  border-radius: 8px;
  background: rgba(255,255,255,0.028);
  color: var(--muted);
  font-size: 11px;
  line-height: 1.25;
}
.table-metric strong {
  display: block;
  margin-top: 2px;
  color: var(--ink);
  font-size: 12px;
}
.influence-table {
  border-collapse: separate;
  border-spacing: 0;
}
.influence-node-table {
  width: 100%;
  min-width: 1320px;
  table-layout: fixed;
}
.influence-node-table th:nth-child(1),
.influence-node-table td:nth-child(1) {
  width: 19%;
  min-width: 0;
  max-width: none;
}
.influence-node-table th:nth-child(2),
.influence-node-table td:nth-child(2) {
  width: 10%;
  min-width: 0;
  max-width: none;
}
.influence-node-table th:nth-child(3),
.influence-node-table td:nth-child(3) {
  width: 15%;
  min-width: 0;
  max-width: none;
}
.influence-node-table th:nth-child(4),
.influence-node-table td:nth-child(4) {
  width: 23%;
  min-width: 0;
  max-width: none;
}
.influence-node-table th:nth-child(5),
.influence-node-table td:nth-child(5),
.influence-node-table th:nth-child(6),
.influence-node-table td:nth-child(6) {
  width: 16.5%;
  min-width: 0;
  max-width: none;
}
.influence-node-table td:nth-child(1) code { max-width: 100%; }
.influence-edge-table {
  min-width: 980px;
}
.influence-edge-table th:nth-child(1),
.influence-edge-table td:nth-child(1),
.influence-edge-table th:nth-child(2),
.influence-edge-table td:nth-child(2) {
  min-width: 220px;
  max-width: 280px;
}
.influence-edge-table th:nth-child(3),
.influence-edge-table td:nth-child(3),
.influence-edge-table th:nth-child(4),
.influence-edge-table td:nth-child(4),
.influence-edge-table th:nth-child(5),
.influence-edge-table td:nth-child(5) {
  min-width: 120px;
  max-width: 140px;
  white-space: nowrap;
}
.influence-edge-table th:nth-child(6),
.influence-edge-table td:nth-child(6) {
  min-width: 320px;
  max-width: 460px;
}
@media (max-width: 820px) {
  .hero { padding: 28px 18px 18px; }
  .hero-grid, .report-hero-lower, .split, .triad, .detail-grid { grid-template-columns: 1fr; }
  .hero-side { width: 100%; }
  .hero-meta { grid-template-columns: 1fr; }
  .report-hero-control { grid-template-rows: auto; }
  .env-grid { grid-template-columns: 1fr; }
  main { padding: 18px; }
  .panel-head { display: block; }
  .chart-row { grid-template-columns: 1fr; }
  details.log-card summary { grid-template-columns: 1fr; }
  .summary-metrics { justify-content: flex-start; }
  table { min-width: 720px; }
  .code-problem-table:not(.leak-table) { min-width: 920px; }
  .leak-table { min-width: 1320px; }
  .code-problem-detail-body { grid-template-columns: 1fr; }
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

const reportJS = `
(() => {
  const markScrollableTables = () => {
    document.querySelectorAll('.table-scroll').forEach((wrapper) => {
      wrapper.classList.toggle('is-scrollable', wrapper.scrollWidth > wrapper.clientWidth + 4);
    });
  };
  const scheduleTableMeasure = () => requestAnimationFrame(markScrollableTables);

  const wrapTables = () => {
    document.querySelectorAll('table').forEach((table) => {
      if (table.closest('.table-scroll')) return;
      const wrapper = document.createElement('div');
      wrapper.className = 'table-scroll';
      table.parentNode.insertBefore(wrapper, table);
      wrapper.appendChild(table);
    });
    scheduleTableMeasure();
  };
  wrapTables();

  const runIdle = (callback) => {
    if ('requestIdleCallback' in window) {
      window.requestIdleCallback(callback, { timeout: 700 });
      return;
    }
    window.setTimeout(() => callback({ timeRemaining: () => 8 }), 0);
  };

  const forEachChunk = (nodes, chunkSize, visit, done) => {
    let index = 0;
    const step = (deadline) => {
      const start = Date.now();
      while (
        index < nodes.length &&
        (index % chunkSize !== 0 ||
          deadline.timeRemaining() > 2 ||
          Date.now() - start < 12)
      ) {
        visit(nodes[index]);
        index += 1;
      }
      if (index < nodes.length) {
        runIdle(step);
      } else if (done) {
        done();
      }
    };
    runIdle(step);
  };

  document.querySelectorAll('code').forEach((node) => {
    const text = node.textContent.trim();
    if (text && !node.title) node.title = text;
    if (text && node.closest('.metric') && !node.dataset.tip) {
      node.dataset.tip = text;
    }
  });

  forEachChunk(Array.from(document.querySelectorAll('td, th')), 300, (node) => {
    const text = node.textContent.trim().replace(/\\s+/g, ' ');
    if (text.length > 80 && !node.dataset.tip) {
      node.dataset.tip = text;
    }
  });

  const enhanceLongCells = () => {
    const cells = Array.from(document.querySelectorAll('.table-scroll td'));
    forEachChunk(cells, 180, (cell) => {
      if (cell.dataset.cellEnhanced === 'true') return;
      if (cell.querySelector('table, canvas, svg, input, select, textarea, details, .cell-toggle')) return;
      const text = cell.textContent.trim().replace(/\\s+/g, ' ');
      const overflows = cell.scrollWidth > cell.clientWidth + 4 || cell.scrollHeight > 180;
      if (text.length < 120 && !overflows) return;
      const clip = document.createElement('div');
      clip.className = 'table-cell-clip';
      while (cell.firstChild) {
        clip.appendChild(cell.firstChild);
      }
      const toggle = document.createElement('button');
      toggle.type = 'button';
      toggle.className = 'cell-toggle';
      toggle.textContent = 'показать полностью';
      toggle.setAttribute('aria-expanded', 'false');
      toggle.addEventListener('click', () => {
        const expanded = !clip.classList.contains('is-expanded');
        clip.classList.toggle('is-expanded', expanded);
        toggle.textContent = expanded ? 'свернуть' : 'показать полностью';
        toggle.setAttribute('aria-expanded', String(expanded));
        scheduleTableMeasure();
      });
      cell.append(clip, toggle);
      cell.dataset.cellEnhanced = 'true';
    }, scheduleTableMeasure);
  };
  enhanceLongCells();

  const tooltip = document.createElement('div');
  tooltip.className = 'jh-tooltip';
  document.body.appendChild(tooltip);
  let activeTarget = null;
  const gap = 10;
  const margin = 12;

  const clamp = (value, min, max) => Math.min(Math.max(value, min), max);

  const viewportBox = () => {
    const viewport = window.visualViewport;
    if (!viewport) {
      return { left: 0, top: 0, right: window.innerWidth, bottom: window.innerHeight };
    }
    return {
      left: viewport.offsetLeft,
      top: viewport.offsetTop,
      right: viewport.offsetLeft + viewport.width,
      bottom: viewport.offsetTop + viewport.height,
    };
  };

  const placeTooltip = (target) => {
    const text = target.dataset.tip || target.getAttribute('aria-label') || target.title || '';
    if (!text) {
      hideTooltip();
      return;
    }
    tooltip.textContent = text;
    tooltip.classList.add('is-visible');
    const rect = target.getBoundingClientRect();
    const tipRect = tooltip.getBoundingClientRect();
    const viewport = viewportBox();
    const centerLeft = rect.left + rect.width / 2 - tipRect.width / 2;
    const middleTop = rect.top + rect.height / 2 - tipRect.height / 2;
    const placements = [
      { name: 'top', left: centerLeft, top: rect.top - tipRect.height - gap },
      { name: 'right', left: rect.right + gap, top: middleTop },
      { name: 'bottom', left: centerLeft, top: rect.bottom + gap },
      { name: 'left', left: rect.left - tipRect.width - gap, top: middleTop },
    ];
    const fits = (placement) =>
      placement.left >= viewport.left + margin &&
      placement.top >= viewport.top + margin &&
      placement.left + tipRect.width <= viewport.right - margin &&
      placement.top + tipRect.height <= viewport.bottom - margin;
    const placement = placements.find(fits) || placements[2];
    const maxLeft = Math.max(viewport.left + margin, viewport.right - tipRect.width - margin);
    const maxTop = Math.max(viewport.top + margin, viewport.bottom - tipRect.height - margin);
    const left = clamp(placement.left, viewport.left + margin, maxLeft);
    const top = clamp(placement.top, viewport.top + margin, maxTop);
    tooltip.dataset.placement = placement.name;
    tooltip.style.left = left + 'px';
    tooltip.style.top = top + 'px';
  };

  const showTooltip = (target) => {
    activeTarget = target;
    placeTooltip(target);
  };

  const hideTooltip = () => {
    activeTarget = null;
    tooltip.classList.remove('is-visible');
  };

  document.addEventListener('pointerover', (event) => {
    const target = event.target.closest('[data-tip]');
    if (target) showTooltip(target);
  });
  document.addEventListener('pointermove', () => {
    if (activeTarget) placeTooltip(activeTarget);
  });
  document.addEventListener('pointerout', (event) => {
    const fromTarget = event.target.closest('[data-tip]');
    const toTarget = event.relatedTarget && event.relatedTarget.closest
      ? event.relatedTarget.closest('[data-tip]')
      : null;
    if (fromTarget && fromTarget === activeTarget && !toTarget) {
      hideTooltip();
    }
  });
  document.addEventListener('focusin', (event) => {
    const target = event.target.closest('[data-tip]');
    if (target) showTooltip(target);
  });
  document.addEventListener('focusout', hideTooltip);
  window.addEventListener('scroll', () => {
    if (activeTarget) placeTooltip(activeTarget);
  }, { passive: true });
  window.addEventListener('resize', () => {
    if (activeTarget) placeTooltip(activeTarget);
  }, { passive: true });
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', () => {
      if (activeTarget) placeTooltip(activeTarget);
    }, { passive: true });
  }

  document.querySelectorAll('[data-zero-toggle]').forEach((toggle) => {
    const scope = toggle.closest('[data-zero-scope]') || document.body;
    const apply = () => scope.classList.toggle('show-zero-buckets', toggle.checked);
    toggle.addEventListener('change', apply);
    apply();
  });

  document.querySelectorAll('[data-code-registry]').forEach((registry) => {
    const tbody = registry.querySelector('tbody');
    const rows = Array.from(registry.querySelectorAll('[data-code-problem-row]'));
    const search = registry.querySelector('[data-code-registry-search]');
    const severity = registry.querySelector('[data-code-registry-severity]');
    const category = registry.querySelector('[data-code-registry-category]');
    const counter = registry.querySelector('[data-code-registry-count]');
    const sortButtons = Array.from(registry.querySelectorAll('[data-code-sort]'));
    const registryScope = registry.closest('.fold-body, .panel, .details-body, .report-section') || registry.parentElement || registry;
    const categoryButtons = Array.from(registryScope.querySelectorAll('[data-registry-category]'));
    const severityButtons = Array.from(registryScope.querySelectorAll('[data-registry-severity]'));
    const severityRank = { high: 3, medium: 2, ok: 1 };
    let sortKey = 'score';
    let sortDir = 'desc';
    const valueFor = (row, key) => {
      if (key === 'score') return Number(row.dataset.score || 0);
      if (key === 'severity') return severityRank[row.dataset.severity] || 0;
      if (key === 'class') return row.dataset.class || '';
      if (key === 'category') return row.dataset.categories || '';
      return row.dataset.search || '';
    };
    const compareValues = (a, b) => {
      const av = valueFor(a, sortKey);
      const bv = valueFor(b, sortKey);
      if (typeof av === 'number' && typeof bv === 'number') return av - bv;
      return String(av).localeCompare(String(bv), 'ru');
    };
    const apply = () => {
      const query = (search?.value || '').trim().toLowerCase();
      const severityValue = severity?.value || '';
      const categoryValue = category?.value || '';
      const sorted = rows.slice().sort((a, b) => {
        const result = compareValues(a, b);
        return sortDir === 'asc' ? result : -result;
      });
      let visible = 0;
      sorted.forEach((row) => {
        const matchesQuery = !query || (row.dataset.search || '').includes(query);
        const matchesSeverity = !severityValue || row.dataset.severity === severityValue;
        const matchesCategory = !categoryValue || (row.dataset.categories || '').split('|').includes(categoryValue);
        const hidden = !(matchesQuery && matchesSeverity && matchesCategory);
        row.hidden = hidden;
        if (!hidden) visible += 1;
        tbody.appendChild(row);
      });
      registry.classList.toggle('no-results', visible === 0);
      if (counter) counter.textContent = visible + ' из ' + rows.length;
      categoryButtons.forEach((button) => {
        button.classList.toggle('is-active', Boolean(categoryValue) && button.dataset.registryCategory === categoryValue);
      });
      severityButtons.forEach((button) => {
        button.classList.toggle('is-active', Boolean(severityValue) && button.dataset.registrySeverity === severityValue);
      });
      sortButtons.forEach((button) => {
        const active = button.dataset.codeSort === sortKey;
        button.classList.toggle('active', active);
        button.classList.toggle('asc', active && sortDir === 'asc');
        button.classList.toggle('desc', active && sortDir === 'desc');
      });
    };
    search?.addEventListener('input', apply);
    severity?.addEventListener('change', apply);
    category?.addEventListener('change', apply);
    categoryButtons.forEach((button) => {
      button.addEventListener('click', () => {
        if (!category) return;
        const value = button.dataset.registryCategory || '';
        category.value = category.value === value ? '' : value;
        apply();
      });
    });
    severityButtons.forEach((button) => {
      button.addEventListener('click', () => {
        if (!severity) return;
        const value = button.dataset.registrySeverity || '';
        severity.value = severity.value === value ? '' : value;
        apply();
      });
    });
    sortButtons.forEach((button) => {
      button.addEventListener('click', () => {
        const nextKey = button.dataset.codeSort;
        if (sortKey === nextKey) {
          sortDir = sortDir === 'asc' ? 'desc' : 'asc';
        } else {
          sortKey = nextKey;
          sortDir = nextKey === 'class' || nextKey === 'category' ? 'asc' : 'desc';
        }
        apply();
      });
    });
    apply();
  });
})();
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
.timeline-table {
  min-width: 1180px;
  table-layout: auto;
  display: table;
}
.timeline-table th,
.timeline-table td {
  min-width: 92px;
  white-space: nowrap;
  overflow-wrap: normal;
  word-break: keep-all;
}
.timeline-table th:first-child,
.timeline-table td:first-child {
  min-width: 150px;
}
.timeline-table td:last-child {
  white-space: nowrap;
  overflow-wrap: normal;
}
.timeline-table code {
  overflow-wrap: normal;
  word-break: keep-all;
  white-space: nowrap;
}
.timeline-table td:last-child code { white-space: nowrap; }
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
.zero-bucket-scope.show-zero-buckets .bucket-zero,
.show-zero-buckets .bucket-zero { display: table-row; }
.zero-toggle-note {
  display: inline-block;
  margin-left: 10px;
  color: var(--muted);
  font-size: 12px;
}
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
.influence-graph-card {
  margin: 10px 0 16px;
  padding: 12px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background:
    linear-gradient(135deg, rgba(98,255,168,0.08), rgba(111,247,255,0.06)),
    rgba(255,255,255,0.026);
  overflow-x: auto;
}
.influence-selection {
  margin: 0 0 10px;
  color: var(--muted);
  font-size: 12px;
}
.influence-selection strong {
  color: var(--ink);
}
.influence-tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin: 0 0 10px;
}
.influence-tools button {
  appearance: none;
  min-height: 34px;
  padding: 7px 12px;
  border: 1px solid rgba(126,247,255,0.22);
  border-radius: 999px;
  color: var(--muted);
  background: rgba(255,255,255,0.035);
  font: 850 11px Inter, "SF Pro Text", Arial, sans-serif;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  cursor: pointer;
}
.influence-tools button:hover,
.influence-tools button.is-active {
  color: #06110b;
  border-color: rgba(98,255,168,0.82);
  background: linear-gradient(135deg, #62ffa8, #c9ff7a);
  box-shadow: 0 0 18px rgba(98,255,168,0.22);
}
.influence-graph {
  width: 100%;
  min-width: 920px;
  height: auto;
  display: block;
}
.influence-layer-label {
  fill: rgba(164,178,201,0.82);
  font: 850 11px Inter, "SF Pro Text", Arial, sans-serif;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.influence-edge {
  stroke: rgba(111,247,255,0.42);
  stroke-linecap: round;
  fill: none;
  transition: opacity 140ms ease, stroke 140ms ease, stroke-width 140ms ease, filter 140ms ease;
}
.influence-edge.confirmed {
  stroke: rgba(98,255,168,0.66);
}
.influence-edge.is-path {
  opacity: 0.98 !important;
  stroke: #62ffa8;
  stroke-width: 4px;
  filter: drop-shadow(0 0 8px rgba(98,255,168,0.52));
}
.influence-edge.is-tree {
  opacity: 0.98 !important;
  stroke: #c9ff7a;
  stroke-width: 4.2px;
  filter: drop-shadow(0 0 10px rgba(201,255,122,0.46));
}
.influence-edge.is-dimmed {
  opacity: 0.08 !important;
}
.influence-node {
  cursor: pointer;
  outline: none;
}
.influence-node .node-card {
  fill: rgba(13,24,43,0.94);
  stroke: rgba(111,247,255,0.38);
  stroke-width: 1.4;
  rx: 12;
  filter: drop-shadow(0 12px 26px rgba(0,0,0,0.26));
  transition: opacity 140ms ease, fill 140ms ease, stroke 140ms ease, stroke-width 140ms ease, filter 140ms ease;
}
.influence-node circle {
  fill: rgba(111,247,255,0.24);
  stroke: rgba(111,247,255,0.94);
  stroke-width: 2.2;
  filter: drop-shadow(0 0 12px rgba(111,247,255,0.36));
  transition: opacity 140ms ease, fill 140ms ease, stroke 140ms ease, stroke-width 140ms ease, filter 140ms ease;
}
.influence-node.high .node-card {
  stroke: rgba(255,148,170,0.58);
}
.influence-node.high circle {
  fill: rgba(255,91,124,0.44);
  stroke: #ff94aa;
  filter: drop-shadow(0 0 18px rgba(255,91,124,0.58));
}
.influence-node.medium .node-card {
  stroke: rgba(255,224,138,0.48);
}
.influence-node.medium circle {
  fill: rgba(255,209,102,0.38);
  stroke: #ffe08a;
  filter: drop-shadow(0 0 16px rgba(255,209,102,0.46));
}
.influence-node.ok .node-card {
  stroke: rgba(155,255,198,0.34);
}
.influence-node.ok circle {
  fill: rgba(98,255,168,0.30);
  stroke: #9bffc6;
  filter: drop-shadow(0 0 14px rgba(98,255,168,0.42));
}
.influence-node.static-only .node-card,
.influence-node.static-only circle {
  stroke-dasharray: 4 4;
  opacity: 0.78;
}
.influence-node text {
  fill: #f7fbff;
  stroke: rgba(3,8,16,0.92);
  stroke-width: 3px;
  paint-order: stroke;
  font: 750 10px Inter, "SF Pro Text", Arial, sans-serif;
  pointer-events: none;
}
.influence-node .node-score-text {
  text-anchor: middle;
  font-size: 8px;
  font-weight: 900;
}
.influence-node .node-label {
  font-size: 11px;
  font-weight: 850;
}
.influence-node .node-kind,
.influence-node .node-reason {
  fill: var(--muted);
  font-size: 8.5px;
  font-weight: 760;
}
.influence-node.is-selected circle {
  stroke: white;
  stroke-width: 3.4;
  filter: drop-shadow(0 0 24px rgba(255,255,255,0.46));
}
.influence-node.is-selected .node-card {
  stroke: white;
  stroke-width: 2.2;
  filter: drop-shadow(0 0 24px rgba(255,255,255,0.34));
}
.influence-node.is-related circle {
  stroke: #62ffa8;
  stroke-width: 2.8;
  filter: drop-shadow(0 0 18px rgba(98,255,168,0.56));
}
.influence-node.is-neighbor circle {
  stroke: #6ff7ff;
  stroke-width: 2.9;
  filter: drop-shadow(0 0 18px rgba(111,247,255,0.50));
}
.influence-node.is-related .node-card {
  stroke: #62ffa8;
  stroke-width: 1.9;
  filter: drop-shadow(0 0 18px rgba(98,255,168,0.26));
}
.influence-node.is-neighbor .node-card {
  stroke: #6ff7ff;
  stroke-width: 1.9;
  filter: drop-shadow(0 0 18px rgba(111,247,255,0.22));
}
.influence-node.is-dimmed .node-card,
.influence-node.is-dimmed circle,
.influence-node.is-dimmed text {
  opacity: 0.18;
}
.heuristic-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}
.heuristic-card {
  min-width: 0;
  overflow: hidden;
  border: 1px solid var(--line);
  border-left: 4px solid var(--ok);
  border-radius: 8px;
  padding: 12px;
  background: rgba(255,255,255,0.032);
}
.heuristic-card.sev-high { border-left-color: var(--bad); }
.heuristic-card.sev-medium { border-left-color: var(--warn); }
.heuristic-card strong { display: block; margin-bottom: 4px; }
.heuristic-card .muted { overflow-wrap: break-word; }
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
<body class="math-page{{if .PresentationMode}} presentation-page{{end}}">
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
      {{if .InfluenceReportHref}}<div class="hero-actions"><a class="math-link" href="{{.InfluenceReportHref}}">Граф влияния</a></div>{{end}}
    </div>
  </div>
</header>
<nav class="nav">
  {{range .Math.Sections}}<a href="#{{.ID}}">{{.Title}}</a>{{end}}
  <a href="#code-problems">Реестр кода</a>
  <a href="#memory-leaks">Утечки</a>
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
    <div class="report-guide">
      <div class="guide-card"><strong>Как читать оценки</strong>Числа вроде 28.04 или 4.86 — не абсолютная “оценка приложения”, а приоритет расследования внутри текущего прогона. Больше обычно важнее, если речь о задержках, памяти, подтормаживаниях или сетевых циклах.</div>
      <div class="guide-card"><strong>С чего начинать</strong>Сначала откройте итоговую эвристику, затем разделы со статусом “критично”, после этого проверьте флоу, проблемные окна и граф влияния кода.</div>
      <div class="guide-card"><strong>Как связывать данные</strong>Смотрите не одну метрику, а цепочку: экран → флоу → шаг → источник → маршрут/пауза/память/спам. Так отчет подсказывает место, где стоит искать причину.</div>
    </div>
    {{scoreGuide "math"}}
    <details id="code-problems" class="fold code-registry-fold" open>
      <summary><span>Реестр проблем кода</span></summary>
      <div class="fold-body">
        <p class="help-text">Реестр собирает классы и методы, где совпали математические сигналы: главный поток, UI, сеть, память, логи, флоу и граф влияния. Используйте его как ранжированный список мест для расследования.</p>
        {{scoreGuide "code"}}
        {{with codeProblemCategories .Math.Summary.CodeProblems}}
        <div class="registry-insights"><span class="registry-insights-label">Категории</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-category="{{.Name}}">{{.Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
        {{end}}
        {{with codeProblemSeverities .Math.Summary.CodeProblems}}
        <div class="registry-insights"><span class="registry-insights-label">Риск</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-severity="{{.Name}}">{{severityLabel .Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
        {{end}}
        <div class="code-registry" data-code-registry>
          <div class="registry-toolbar">
            <input type="search" data-code-registry-search placeholder="Фильтр по классу, методу, экрану, флоу, маршруту или проблеме" aria-label="Фильтр реестра проблем кода">
            <select data-code-registry-severity aria-label="Фильтр по риску">
              <option value="">Все уровни риска</option>
              <option value="high">Критично</option>
              <option value="medium">Предупреждение</option>
              <option value="ok">Низкий риск</option>
            </select>
            <select data-code-registry-category aria-label="Фильтр по категории">
              <option value="">Все категории</option>
              <option value="Сеть">Сеть</option>
              <option value="UI">UI</option>
              <option value="Главный поток">Главный поток</option>
              <option value="Память">Память</option>
              <option value="Логи">Логи</option>
              <option value="Выполнение">Выполнение</option>
              <option value="Граф влияния">Граф влияния</option>
              <option value="ANR-risk">ANR-risk</option>
              <option value="OOM-risk">OOM-risk</option>
              <option value="GC pressure">GC pressure</option>
              <option value="duplicate network">duplicate network</option>
              <option value="lifecycle leak">lifecycle leak</option>
              <option value="log spam">log spam</option>
              <option value="main-thread IO">main-thread IO</option>
            </select>
            <span class="registry-counter" data-code-registry-count></span>
          </div>
          <div class="problem-empty">По текущим фильтрам проблемных классов не найдено.</div>
          <table class="code-problem-table">
            <thead>
              <tr>
                <th><button type="button" data-code-sort="score">Оценка</button></th>
                <th><button type="button" data-code-sort="class">Класс / метод</button></th>
                <th><button type="button" data-code-sort="category">Категории</button></th>
                <th>Подробности</th>
              </tr>
            </thead>
            <tbody>
              {{range .Math.Summary.CodeProblems}}
              <tr data-code-problem-row data-score="{{printf "%.1f" .Score}}" data-severity="{{.Severity}}" data-class="{{codeProblemLocation .}}" data-categories="{{join .Categories "|"}}" data-search="{{codeProblemSearchText .}}">
                <td><span class="problem-score {{severityClass .Severity}}">{{printf "%.1f" .Score}}</span><div class="muted">{{severityLabel .Severity}}</div>{{if .RuntimeEvidence}}<div class="muted">есть выполнение</div>{{else}}<div class="muted">статический след</div>{{end}}</td>
                <td><div class="problem-location"><code>{{.ClassName}}</code>{{if .Method}}<div class="method"><code>{{.Method}}</code></div>{{end}}</div></td>
                <td><div class="problem-tags">{{range .Categories}}<span class="problem-chip">{{.}}</span>{{end}}</div><div class="muted">{{join .Problems ", "}}</div></td>
                <td>
                  <details class="code-problem-details">
                    <summary><span class="code-problem-summary-main"><strong>Доказательства и рекомендация</strong><em>{{.Evidence}}</em></span><small>{{len .Signals}} сигналов{{if .DrillDown}} · {{len .DrillDown}} флоу{{end}}</small></summary>
                    <div class="code-problem-detail-body">
                      <div class="code-problem-detail-block span-all"><strong>Доказательство</strong><p>{{.Evidence}}</p></div>
                      <div class="code-problem-detail-block"><strong>Контекст</strong><div class="problem-context">{{range .Screens}}<div>экран <code>{{.}}</code></div>{{end}}{{range .Flows}}<div>флоу <code>{{.}}</code></div>{{end}}{{range .Steps}}<div>шаг <code>{{.}}</code></div>{{end}}{{range .Routes}}<div>маршрут <code>{{.}}</code></div>{{end}}</div></div>
                      <div class="code-problem-detail-block"><strong>Влияние</strong><p>{{.Impact}}</p></div>
                      <div class="code-problem-detail-block span-all"><strong>Что проверить</strong><p>{{.Recommendation}}</p></div>
                      {{if .DrillDown}}<div class="code-problem-detail-block span-all"><strong>Drill-down</strong><div class="problem-drilldown">{{range .DrillDown}}<div class="problem-drill"><strong>{{codeProblemDrillPath .}}</strong><span>Доказательство: {{.Evidence}}</span><span>Рекомендация: {{.Recommendation}}</span></div>{{end}}</div></div>{{end}}
                      <div class="code-problem-detail-block span-all"><strong>Сигналы</strong><div class="problem-signals">{{range .Signals}}<div class="problem-signal {{severityClass .Severity}}"><strong>{{.Name}}</strong><small>{{.Category}} · {{codeProblemMetric .}}<br>{{.Detail}}</small></div>{{end}}</div></div>
                    </div>
                  </details>
                </td>
              </tr>
              {{else}}<tr><td colspan="4" class="muted">Реестр проблем кода пуст: текущий прогон не дал привязанных к классу сигналов. Проверьте ASM-опции owners, flowInteractions, runtimeCallGraph и logSpam.</td></tr>{{end}}
            </tbody>
          </table>
        </div>
      </div>
    </details>
    <details id="memory-leaks" class="fold code-registry-fold">
      <summary><span>Разбор утечек памяти</span></summary>
      <div class="fold-body">
        <p class="help-text">Показывает удержанные объекты с вероятным держателем и контекстом. Системные классы выводятся только как симптом, основной фокус — пользовательский держатель, экран и флоу.</p>
        <div class="leak-limitations">Точный путь до корня GC требует дампа памяти. В легком режиме выполнения отчет показывает подсказку владельца, текущий контекст или место вызова watch-метода и честно помечает строки, где держатель не определен.</div>
        {{scoreGuide "leak"}}
        <div class="code-registry" data-code-registry>
          <div class="registry-toolbar">
            <input type="search" data-code-registry-search placeholder="Фильтр по классу, держателю, экрану, флоу или рекомендации" aria-label="Фильтр реестра утечек памяти">
            <select data-code-registry-severity aria-label="Фильтр утечек по риску">
              <option value="">Все уровни риска</option>
              <option value="high">Критично</option>
              <option value="medium">Предупреждение</option>
              <option value="ok">Норма</option>
            </select>
            <select data-code-registry-category aria-label="Фильтр утечек по типу объекта">
              <option value="">Все типы</option>
              <option value="экран / Activity">Activity</option>
              <option value="Fragment">Fragment</option>
              <option value="Context">Context</option>
              <option value="View / binding">View / binding</option>
              <option value="ресурс">Ресурс</option>
              <option value="системный объект">Системный объект</option>
              <option value="пользовательский объект">Пользовательский объект</option>
            </select>
            <span class="registry-counter" data-code-registry-count></span>
          </div>
          <div class="problem-empty">По текущим фильтрам подозрений утечек не найдено.</div>
          <table class="code-problem-table leak-table">
            <tr><th><button type="button" data-code-sort="severity">Риск</button></th><th><button type="button" data-code-sort="class">Удержанный объект</button></th><th><button type="button" data-code-sort="category">Тип</button></th><th>Вероятный держатель</th><th>Контекст</th><th><button type="button" data-code-sort="score">Оценка</button></th><th>{{tip "Оценка удержанного размера" "Ориентировочная оценка удержанного размера. Без дампа памяти это не точный размер удержанной кучи, а расчет по типу объекта, числу удержаний, возрасту и PSS процесса."}}</th><th>{{tip "Мини-дерево доминирования" "Вероятная цепочка владения из контекста выполнения: экран, флоу, шаг, держатель и удержанный объект. Точный корень GC требует дампа памяти."}}</th><th>Влияние</th><th>Что проверить</th><th>Доказательства</th></tr>
            {{range .Math.Summary.MemoryLeaks}}
            <tr data-code-problem-row data-score="{{printf "%.1f" .Score}}" data-severity="{{.Severity}}" data-class="{{.ClassName}}" data-categories="{{.ObjectKind}}" data-search="{{memoryLeakSearchText .}}">
              <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span></td>
              <td><code>{{.ClassName}}</code></td>
              <td><div class="leak-object-kind">{{.ObjectKind}}</div>{{if .SystemRetained}}<div class="muted">системный объект</div>{{else if .UserOwned}}<div class="muted">пользовательский код</div>{{end}}</td>
              <td><code>{{.Holder}}</code><div class="leak-holder-quality">{{.HolderQuality}}</div></td>
              <td><div class="problem-context">{{if .Screen}}<div>экран <code>{{.Screen}}</code></div>{{end}}{{if .Flow}}<div>флоу <code>{{.Flow}}</code></div>{{end}}{{if .Step}}<div>шаг <code>{{.Step}}</code></div>{{end}}</div></td>
              <td><div>{{printf "%.1f" .Score}}</div><div class="muted">{{.Count}} шт · {{humanDuration .MaxAgeMS}}</div></td>
              <td><div>{{dataSize .EstimatedRetainedKB}}</div><div class="muted">{{.RetainedSizeConfidence}}</div><div class="muted">{{.RetainedSizeExplanation}}</div></td>
              <td><div class="leak-dominator">{{range .DominatorPath}}<span>{{.}}</span>{{end}}</div><div class="muted">{{.DominatorTreeExplanation}}</div><div class="leak-chain-summary">{{.LeakChainSummary}}</div></td>
              <td>{{.Impact}}</td>
              <td>{{.Recommendation}}<div class="leak-chain-actions"><strong>Быстрые проверки цепочки</strong>{{range .LeakChainActions}}<span>{{.}}</span>{{end}}</div></td>
              <td>{{.Evidence}}</td>
            </tr>
            {{else}}<tr><td colspan="11" class="muted">Подозрений на утечки памяти нет.</td></tr>{{end}}
          </table>
        </div>
      </div>
    </details>
    <details class="fold overview-attribution-fold">
      <summary><span>Атрибуция флоу и причин</span></summary>
      <div class="fold-body">
        <p class="help-text">Здесь собраны длинные таблицы связки “экран → флоу → шаг → источник → маршрут/пауза/память/спам”. Блок свернут по умолчанию, чтобы обзор качества данных не превращался в простыню.</p>
        <h3>Флоу и причины</h3>
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
        <h3>Вызовы выполнения</h3>
        <table class="timeline-table">
          <tr><th>Экран / флоу / шаг</th><th>Откуда</th><th>Куда</th><th>Количество</th><th>Итого</th><th>Макс.</th></tr>
          {{range .Math.Summary.RuntimeCalls}}
          <tr><td><code>{{flowKeyLabel .Screen .Flow .Step ""}}</code></td><td><code>{{.Caller}}</code></td><td><code>{{.Callee}}</code></td><td>{{.Count}}</td><td>{{.TotalMS}} мс</td><td>{{.MaxMS}} мс</td></tr>
          {{else}}<tr><td colspan="6" class="muted">Нет графа вызовов выполнения. Включите ASM-опцию runtimeCallGraph для целевых пакетов.</td></tr>{{end}}
        </table>
        {{if .Math.Summary.Influence.Available}}
        <h3>Граф влияния кода</h3>
        <p class="help-text">Этот блок связывает математические симптомы с классами: оценка растет от сетевых хвостов, пауз главного потока, UI-подтормаживаний, памяти, спама логами и радиуса флоу. Статические связи между классами доступны при передаче ` + "`--class-graph`" + `.</p>
        <table class="timeline-table">
          <tr><th>Класс</th><th>{{tip "Оценка" (scoreHelp "influence")}}</th><th>Риск</th><th>Статус</th><th>Причины</th><th>Флоу</th></tr>
          {{range topInfluenceNodes .Math.Summary.Influence 8}}
          <tr><td><code>{{.ClassName}}</code></td><td>{{printf "%.1f" .Score}}</td><td>{{influenceSeverity .Severity}}</td><td>{{influenceStatus .Status}}</td><td>{{join .Reasons ", "}}</td><td>{{join .Flows ", "}}</td></tr>
          {{end}}
        </table>
        {{if .InfluenceReportHref}}<p class="help-text"><a href="{{.InfluenceReportHref}}">Открыть подробный граф влияния кода</a>. {{.Math.Summary.Influence.StandaloneReason}}</p>{{end}}
        {{end}}
      </div>
    </details>
    <h3>Находки</h3>
    <div class="finding-list">
      {{range significantMathFindings .Math.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
      {{else}}<div class="muted">Нет значимых предупреждений по качеству данных.</div>{{end}}
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
          {{range significantMathFindings .Findings}}
          <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
          {{else}}<div class="muted">Нет значимых предупреждений в этом разделе.</div>{{end}}
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
        <div class="zero-bucket-scope" data-zero-scope>
          <label class="zero-toggle"><input type="checkbox" data-zero-toggle>Показать нулевые интервалы</label><span class="zero-toggle-note">Пустые интервалы скрыты, чтобы таймлайн показывал только полезные участки.</span>
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
          <tr><th>Сигнал</th><th>Время</th><th>Направление</th><th>До</th><th>После</th><th>Δ</th><th>Δ%</th><th>MAD до/после</th><th>{{tip "Оценка" (scoreHelp "change")}}</th><th>Контекст</th><th>Рекомендация</th></tr>
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
          <tr><th>Маршрут</th><th>Источник</th><th>Период</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th><th>{{tip "Выгорание" (scoreHelp "network_burn")}}</th><th>Окно</th><th>Паттерн</th><th>Вероятная причина</th><th>Путь</th></tr>
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
          <tr><th>{{tip "Оценка" (scoreHelp "integral")}}</th><th>Значение</th><th>Критерии</th><th>Формула</th><th>Что измеряет</th><th>Как читать</th></tr>
          {{range $math.IntegralScores}}
          <tr>
            <td>{{.Title}}</td>
            <td><span class="section-status {{severityClass .Severity}}">{{printf "%.1f" .Value}} {{.Unit}}</span></td>
            <td>{{integralCriteria .ID}}</td>
            <td><code>{{.Formula}}</code></td>
            <td>{{.Explanation}}</td>
            <td>{{integralHelp .ID}}</td>
          </tr>
          {{else}}<tr><td colspan="6" class="muted">Недостаточно данных для интегральных оценок.</td></tr>{{end}}
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
          {{range significantMarkovStates $math.Markov.States}}
          <tr>
            <td>{{printf "%.1fs" (seconds .TimeMS)}}</td>
            <td>{{markovState .State}}</td>
            <td>{{.Reason}}</td>
            <td>{{if .Screen}}экран <code>{{.Screen}}</code><br>{{end}}{{if .Route}}маршрут <code>{{.Route}}</code><br>{{end}}{{if .Owner}}источник <code>{{.Owner}}</code><br>{{end}}{{if .Network}}сеть <code>{{.Network}}</code>{{end}}</td>
          </tr>
          {{else}}<tr><td colspan="4" class="muted">Нет значимых плохих или восстановительных интервалов.</td></tr>{{end}}
          {{if hiddenMarkovStates $math.Markov.States}}<tr><td colspan="4" class="muted">Скрыто спокойных интервалов: {{hiddenMarkovStates $math.Markov.States}}.</td></tr>{{end}}
        </table>
        <h3>Матрица переходов</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Количество</th><th>Вероятность</th></tr>
          {{range significantMarkovTransitions $math.Markov.Transitions}}
          <tr><td>{{markovState .From}}</td><td>{{markovState .To}}</td><td>{{.Count}}</td><td>{{printf "%.1f" (percent01 .Probability)}}%</td></tr>
          {{else}}<tr><td colspan="4" class="muted">Нет значимых переходов между состояниями деградации.</td></tr>{{end}}
          {{if hiddenMarkovTransitions $math.Markov.Transitions}}<tr><td colspan="4" class="muted">Скрыто спокойных переходов: {{hiddenMarkovTransitions $math.Markov.Transitions}}.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "graph"}}
        <h3>Визуальный граф</h3>
        <p class="help-text">Показаны только самые сильные связи, чтобы большой проект не превращал граф в шум. Полные ребра и пути доступны в таблицах ниже.</p>
        {{causalGraphSVG $math.CausalGraph}}
        <h3>Кратчайшие объясняющие пути</h3>
        <table class="timeline-table">
          <tr><th>От</th><th>К</th><th>Путь</th><th>{{tip "Стоимость" (scoreHelp "path_cost")}}</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th></tr>
          {{range $math.CausalGraph.Paths}}
          <tr><td>{{.From}}</td><td>{{.To}}</td><td>{{pathText .}}</td><td>{{printf "%.2f" .Cost}}</td><td>{{printf "%.2f" .Confidence}}</td></tr>
          {{else}}<tr><td colspan="5" class="muted">Кратчайшие пути от симптомов к источникам/маршрутам не найдены.</td></tr>{{end}}
        </table>
        <h3>Вклад источников</h3>
        <table class="timeline-table">
          <tr><th>Ранг</th><th>Источник</th><th>{{tip "Оценка" (scoreHelp "influence")}}</th></tr>
          {{range $math.CausalGraph.OwnerScores}}
          <tr><td>{{.Rank}}</td><td><code>{{.Owner}}</code></td><td>{{printf "%.2f" .Score}}</td></tr>
          {{else}}<tr><td colspan="3" class="muted">Источники не выделены.</td></tr>{{end}}
        </table>
        <h3>Ребра графа</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Тип</th><th>Наблюдения</th><th>Вес</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th><th>Описание</th></tr>
          {{range $math.CausalGraph.Edges}}
          <tr><td>{{.FromLabel}}</td><td>{{.ToLabel}}</td><td>{{causalKind .Kind}}</td><td>{{.Count}}</td><td>{{printf "%.2f" .Weight}}</td><td>{{printf "%.2f" .Confidence}}</td><td>{{.Description}}</td></tr>
          {{else}}<tr><td colspan="7" class="muted">Ребра недоступны.</td></tr>{{end}}
        </table>
        <h3>Все кратчайшие пары Floyd-Warshall</h3>
        <table class="timeline-table">
          <tr><th>От</th><th>К</th><th>Путь</th><th>{{tip "Стоимость" (scoreHelp "path_cost")}}</th></tr>
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
<script>` + reportJS + `
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
<body class="math-page{{if .PresentationMode}} presentation-page{{end}}">
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
      {{if .InfluenceReportHref}}<div class="hero-actions"><a class="math-link" href="{{.InfluenceReportHref}}">Граф влияния</a></div>{{end}}
    </div>
  </div>
</header>
<nav class="nav">
  {{range .Math.Sections}}<a href="#{{.ID}}">{{.Title}}</a>{{end}}
  <a href="#code-problems">Реестр кода</a>
  <a href="#memory-leaks">Утечки</a>
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
    <div class="report-guide">
      <div class="guide-card"><strong>Как читать сравнение</strong>Положительная дельта у задержек, памяти, подтормаживаний, ошибок и спама обычно означает ухудшение кандидата. Смотрите серьезность вместе с доверием и размером выборки.</div>
      <div class="guide-card"><strong>Где искать причину</strong>После общей регрессии откройте “Флоу и причины”, затем “Граф влияния кандидата”: там видно, какие источники и маршруты связаны с ухудшением.</div>
      <div class="guide-card"><strong>Что считать выводом</strong>Один большой пик без повторяемости — повод проверить вручную. Повторяемый сдвиг, сетевой цикл или высокий интеграл — уже сильный сигнал для задачи на исправление.</div>
    </div>
    {{scoreGuide "compare"}}
    <details id="code-problems" class="fold code-registry-fold" open>
      <summary><span>Реестр проблем кода кандидата</span></summary>
      <div class="fold-body">
        <p class="help-text">Показывает классы и методы кандидата, которые стали заметнее относительно базы или уже несут высокий риск. Дельта оценки помогает быстро отделить новые регрессии от старого технического долга.</p>
        {{scoreGuide "code"}}
        {{with codeProblemCategories .Math.Candidate.Summary.CodeProblems}}
        <div class="registry-insights"><span class="registry-insights-label">Категории</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-category="{{.Name}}">{{.Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
        {{end}}
        {{with codeProblemSeverities .Math.Candidate.Summary.CodeProblems}}
        <div class="registry-insights"><span class="registry-insights-label">Риск</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-severity="{{.Name}}">{{severityLabel .Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
        {{end}}
        <div class="code-registry" data-code-registry>
          <div class="registry-toolbar">
            <input type="search" data-code-registry-search placeholder="Фильтр по классу, методу, экрану, флоу, маршруту или проблеме" aria-label="Фильтр сравнительного реестра проблем кода">
            <select data-code-registry-severity aria-label="Фильтр по риску">
              <option value="">Все уровни риска</option>
              <option value="high">Критично</option>
              <option value="medium">Предупреждение</option>
              <option value="ok">Норма</option>
            </select>
            <select data-code-registry-category aria-label="Фильтр по категории">
              <option value="">Все категории</option>
              <option value="Сеть">Сеть</option>
              <option value="UI">UI</option>
              <option value="Главный поток">Главный поток</option>
              <option value="Память">Память</option>
              <option value="Логи">Логи</option>
              <option value="Выполнение">Выполнение</option>
              <option value="Граф влияния">Граф влияния</option>
              <option value="ANR-risk">ANR-risk</option>
              <option value="OOM-risk">OOM-risk</option>
              <option value="GC pressure">GC pressure</option>
              <option value="duplicate network">duplicate network</option>
              <option value="lifecycle leak">lifecycle leak</option>
              <option value="log spam">log spam</option>
              <option value="main-thread IO">main-thread IO</option>
            </select>
            <span class="registry-counter" data-code-registry-count></span>
          </div>
          <div class="problem-empty">По текущим фильтрам проблемных классов не найдено.</div>
          <table class="code-problem-table">
            <thead>
              <tr>
                <th><button type="button" data-code-sort="severity">Статус</button></th>
                <th><button type="button" data-code-sort="class">Класс / метод</button></th>
                <th><button type="button" data-code-sort="score">Оценка</button></th>
                <th><button type="button" data-code-sort="category">Категории</button></th>
                <th>Подробности кандидата</th>
              </tr>
            </thead>
            <tbody>
              {{range codeProblemCompareRows .Math.Comparison}}
              <tr data-code-problem-row data-score="{{printf "%.1f" .DeltaScore}}" data-severity="{{.Severity}}" data-class="{{codeProblemLocation .Candidate}}" data-categories="{{join .Candidate.Categories "|"}}" data-search="{{codeProblemSearchText .Candidate}} {{.Status}}">
                <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span><div class="muted">{{.Status}}</div></td>
                <td><div class="problem-location"><code>{{.Candidate.ClassName}}</code>{{if .Candidate.Method}}<div class="method"><code>{{.Candidate.Method}}</code></div>{{end}}</div></td>
                <td><div>база {{printf "%.1f" .BaselineScore}}</div><div>кандидат {{printf "%.1f" .Candidate.Score}}</div><div class="muted">дельта {{printf "%+.1f" .DeltaScore}}</div></td>
                <td><div class="problem-tags">{{range .Candidate.Categories}}<span class="problem-chip">{{.}}</span>{{end}}</div><div class="muted">{{join .Candidate.Problems ", "}}</div></td>
                <td>
                  <details class="code-problem-details">
                    <summary><span class="code-problem-summary-main"><strong>Доказательства и рекомендация</strong><em>{{.Candidate.Evidence}}</em></span><small>{{len .Candidate.Signals}} сигналов{{if .Candidate.DrillDown}} · {{len .Candidate.DrillDown}} флоу{{end}}</small></summary>
                    <div class="code-problem-detail-body">
                      <div class="code-problem-detail-block span-all"><strong>Доказательство</strong><p>{{.Candidate.Evidence}}</p></div>
                      <div class="code-problem-detail-block"><strong>Контекст</strong><div class="problem-context">{{range .Candidate.Screens}}<div>экран <code>{{.}}</code></div>{{end}}{{range .Candidate.Flows}}<div>флоу <code>{{.}}</code></div>{{end}}{{range .Candidate.Steps}}<div>шаг <code>{{.}}</code></div>{{end}}{{range .Candidate.Routes}}<div>маршрут <code>{{.}}</code></div>{{end}}</div></div>
                      <div class="code-problem-detail-block"><strong>Влияние кандидата</strong><p>{{.Candidate.Impact}}</p></div>
                      <div class="code-problem-detail-block span-all"><strong>Что проверить</strong><p>{{.Candidate.Recommendation}}</p></div>
                      {{if .Candidate.DrillDown}}<div class="code-problem-detail-block span-all"><strong>Drill-down</strong><div class="problem-drilldown">{{range .Candidate.DrillDown}}<div class="problem-drill"><strong>{{codeProblemDrillPath .}}</strong><span>Доказательство: {{.Evidence}}</span><span>Рекомендация: {{.Recommendation}}</span></div>{{end}}</div></div>{{end}}
                      <div class="code-problem-detail-block span-all"><strong>Сигналы кандидата</strong><div class="problem-signals">{{range .Candidate.Signals}}<div class="problem-signal {{severityClass .Severity}}"><strong>{{.Name}}</strong><small>{{.Category}} · {{codeProblemMetric .}}<br>{{.Detail}}</small></div>{{end}}</div></div>
                    </div>
                  </details>
                </td>
              </tr>
              {{else}}<tr><td colspan="5" class="muted">Реестр проблем кода кандидата пуст: в сравнении нет привязанных к классу сигналов.</td></tr>{{end}}
            </tbody>
          </table>
        </div>
      </div>
    </details>
    <details id="memory-leaks" class="fold code-registry-fold">
      <summary><span>Сравнение утечек памяти</span></summary>
      <div class="fold-body">
        <p class="help-text">Показывает удержанные объекты кандидата, которые появились или усилились относительно базы. Оценка учитывает количество, возраст удержания, тип объекта и наличие пользовательского держателя.</p>
        {{scoreGuide "leak"}}
        <div class="code-registry" data-code-registry>
          <div class="registry-toolbar">
            <input type="search" data-code-registry-search placeholder="Фильтр по удержанному объекту, держателю, экрану или рекомендации" aria-label="Фильтр сравнительного реестра утечек памяти">
            <select data-code-registry-severity aria-label="Фильтр утечек по риску">
              <option value="">Все уровни риска</option>
              <option value="high">Критично</option>
              <option value="medium">Предупреждение</option>
              <option value="ok">Норма</option>
            </select>
            <select data-code-registry-category aria-label="Фильтр утечек по типу объекта">
              <option value="">Все типы</option>
              <option value="экран / Activity">Activity</option>
              <option value="Fragment">Fragment</option>
              <option value="Context">Context</option>
              <option value="View / binding">View / binding</option>
              <option value="ресурс">Ресурс</option>
              <option value="системный объект">Системный объект</option>
              <option value="пользовательский объект">Пользовательский объект</option>
            </select>
            <span class="registry-counter" data-code-registry-count></span>
          </div>
          <div class="problem-empty">По текущим фильтрам утечек не найдено.</div>
          <table class="code-problem-table leak-table">
            <thead>
              <tr>
                <th><button type="button" data-code-sort="severity">Риск</button></th>
                <th><button type="button" data-code-sort="class">Удержанный объект</button></th>
                <th><button type="button" data-code-sort="category">Тип</button></th>
                <th>Вероятный держатель</th>
                <th><button type="button" data-code-sort="score">Оценка</button></th>
                <th>Изменение</th>
                <th>Контекст</th>
                <th>{{tip "Оценка удержанного размера" "Ориентировочная оценка удержанного размера кандидата. Точное значение требует дампа памяти."}}</th>
                <th>{{tip "Мини-дерево доминирования" "Вероятная цепочка владения кандидата по контексту выполнения."}}</th>
                <th>Что проверить</th>
                <th>Доказательства</th>
              </tr>
            </thead>
            <tbody>
              {{range memoryLeakCompareRows .Math.Comparison}}
              <tr data-code-problem-row data-score="{{printf "%.1f" .DeltaScore}}" data-severity="{{.Severity}}" data-class="{{.Candidate.ClassName}}" data-categories="{{.Candidate.ObjectKind}}" data-search="{{memoryLeakSearchText .Candidate}} {{.Status}}">
                <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span><div class="muted">{{.Status}}</div></td>
                <td><div class="problem-location"><code>{{.Candidate.ClassName}}</code></div></td>
                <td><div class="leak-object-kind">{{.Candidate.ObjectKind}}</div>{{if .Candidate.SystemRetained}}<div class="muted">системный объект</div>{{else if .Candidate.UserOwned}}<div class="muted">пользовательский код</div>{{end}}</td>
                <td><code>{{.Candidate.Holder}}</code><div class="leak-holder-quality">{{.Candidate.HolderQuality}}</div></td>
                <td><div>база {{printf "%.1f" .BaselineScore}}</div><div>кандидат {{printf "%.1f" .Candidate.Score}}</div><div class="muted">дельта {{printf "%+.1f" .DeltaScore}}</div></td>
                <td><div>кол-во {{.BaselineCount}} → {{.Candidate.Count}} ({{printf "%+d" .DeltaCount}})</div><div>возраст {{humanDuration .BaselineAgeMS}} → {{humanDuration .Candidate.MaxAgeMS}} ({{signedDuration .DeltaAgeMS}})</div></td>
                <td><div class="problem-context">{{if .Candidate.Screen}}<div>экран <code>{{.Candidate.Screen}}</code></div>{{end}}{{if .Candidate.Flow}}<div>флоу <code>{{.Candidate.Flow}}</code></div>{{end}}{{if .Candidate.Step}}<div>шаг <code>{{.Candidate.Step}}</code></div>{{end}}</div></td>
                <td><div>{{dataSize .Candidate.EstimatedRetainedKB}}</div><div class="muted">{{.Candidate.RetainedSizeConfidence}}</div><div class="muted">{{.Candidate.RetainedSizeExplanation}}</div></td>
                <td><div class="leak-dominator">{{range .Candidate.DominatorPath}}<span>{{.}}</span>{{end}}</div><div class="muted">{{.Candidate.DominatorTreeExplanation}}</div><div class="leak-chain-summary">{{.Candidate.LeakChainSummary}}</div></td>
                <td>{{.Candidate.Recommendation}}<div class="leak-chain-actions"><strong>Быстрые проверки цепочки</strong>{{range .Candidate.LeakChainActions}}<span>{{.}}</span>{{end}}</div></td>
                <td>{{.Candidate.Evidence}}</td>
              </tr>
              {{else}}<tr><td colspan="11" class="muted">У кандидата нет подозрений на утечки памяти.</td></tr>{{end}}
            </tbody>
          </table>
        </div>
      </div>
    </details>
    {{if .Math.Candidate.Summary.Influence.Available}}
    <h3>Граф влияния кандидата</h3>
    <p class="help-text">Встроенный срез показывает верхние классы кандидата, а полный граф вынесен в отдельный HTML.</p>
    <table class="timeline-table">
      <tr><th>Класс</th><th>{{tip "Оценка" (scoreHelp "influence")}}</th><th>Риск</th><th>Статус</th><th>Причины</th></tr>
      {{range topInfluenceNodes .Math.Candidate.Summary.Influence 8}}
      <tr><td><code>{{.ClassName}}</code></td><td>{{printf "%.1f" .Score}}</td><td>{{influenceSeverity .Severity}}</td><td>{{influenceStatus .Status}}</td><td>{{join .Reasons ", "}}</td></tr>
      {{end}}
    </table>
    {{if .InfluenceReportHref}}<p class="help-text"><a href="{{.InfluenceReportHref}}">Открыть подробный граф влияния кандидата</a></p>{{end}}
    {{end}}
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
      {{range significantMathFindings .Math.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
      {{else}}<div class="muted">Нет значимых предупреждений по качеству сравнения.</div>{{end}}
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
          {{range significantMathFindings .Findings}}
          <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div>{{if .Recommendation}}<div class="muted">{{.Recommendation}}</div>{{end}}</div>
          {{else}}<div class="muted">Нет значимых предупреждений в этом разделе.</div>{{end}}
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
            <tr><th>Статус</th><th>Имя</th><th>Метрика</th><th>N база</th><th>N кандидат</th><th>p95 база</th><th>p95 кандидат</th><th>Δ p95</th><th>Δ%</th><th>Дельта Клиффа</th><th>Эффект</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th><th>Вывод</th></tr>
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
          <tr><th>Статус</th><th>Сигнал</th><th>Время базы</th><th>Время кандидата</th><th>{{tip "Оценка базы" (scoreHelp "change")}}</th><th>{{tip "Оценка кандидата" (scoreHelp "change")}}</th><th>Вывод</th></tr>
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
          <tr><th>Статус</th><th>Маршрут</th><th>Источник</th><th>Период базы</th><th>Период кандидата</th><th>{{tip "Выгорание базы" (scoreHelp "network_burn")}}</th><th>{{tip "Выгорание кандидата" (scoreHelp "network_burn")}}</th><th>Δ выгорания</th><th>{{tip "Δ доверия" (scoreHelp "confidence")}}</th><th>Вывод</th></tr>
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
          <tr><th>Статус</th><th>{{tip "Оценка" (scoreHelp "integral")}}</th><th>База</th><th>Кандидат</th><th>Δ</th><th>Δ%</th><th>Критерии</th><th>Формула</th><th>Вывод</th></tr>
          {{range $math.IntegralDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Title}}</td>
            <td>{{printf "%.1f" .BaselineValue}} {{.Unit}}</td>
            <td>{{printf "%.1f" .CandidateValue}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .Delta}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .DeltaPct}}%</td>
            <td>{{integralCriteria .ID}}</td>
            <td><code>{{.Formula}}</code></td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="9" class="muted">Интегральные дельты недоступны.</td></tr>{{end}}
        </table>
        {{end}}
        {{if eq .ID "markov"}}
        <h3>Дельты марковских метрик</h3>
        <table class="timeline-table">
          <tr><th>Статус</th><th>Метрика</th><th>База</th><th>Кандидат</th><th>Δ</th><th>Вывод</th></tr>
          {{range significantMarkovDeltas $math.MarkovDeltas}}
          <tr>
            <td><span class="section-status {{severityClass .Severity}}">{{statusLabel .Severity}}</span></td>
            <td>{{.Metric}}</td>
            <td>{{printf "%.1f" .BaselineValue}} {{.Unit}}</td>
            <td>{{printf "%.1f" .CandidateValue}} {{.Unit}}</td>
            <td>{{printf "%+.1f" .Delta}} {{.Unit}}</td>
            <td>{{.Summary}}</td>
          </tr>
          {{else}}<tr><td colspan="6" class="muted">Нет значимых марковских дельт.</td></tr>{{end}}
          {{if hiddenMarkovDeltas $math.MarkovDeltas}}<tr><td colspan="6" class="muted">Скрыто спокойных марковских дельт: {{hiddenMarkovDeltas $math.MarkovDeltas}}.</td></tr>{{end}}
        </table>
        <h3>Переходы кандидата</h3>
        <table class="timeline-table">
          <tr><th>Из</th><th>В</th><th>Количество</th><th>Вероятность</th></tr>
          {{range significantMarkovTransitions $math.Candidate.Markov.Transitions}}
          <tr><td>{{markovState .From}}</td><td>{{markovState .To}}</td><td>{{.Count}}</td><td>{{printf "%.1f" (percent01 .Probability)}}%</td></tr>
          {{else}}<tr><td colspan="4" class="muted">Нет значимых переходов кандидата между состояниями деградации.</td></tr>{{end}}
          {{if hiddenMarkovTransitions $math.Candidate.Markov.Transitions}}<tr><td colspan="4" class="muted">Скрыто спокойных переходов кандидата: {{hiddenMarkovTransitions $math.Candidate.Markov.Transitions}}.</td></tr>{{end}}
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
          <tr><th>От</th><th>К</th><th>Путь</th><th>{{tip "Стоимость" (scoreHelp "path_cost")}}</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th></tr>
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
<script>` + reportJS + `
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
})();
</script>
</body>
</html>`

const influenceTemplate = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jank Hunter: граф влияния кода</title>
  <style>` + baseCSS + mathCSS + `</style>
</head>
<body{{if .PresentationMode}} class="presentation-page"{{end}}>
<header class="hero">
  <div class="hero-grid">
    <div>
      <div class="eyebrow">Jank Hunter · граф</div>
      <h1>{{.Title}}</h1>
      <div class="subhead">создан {{.GeneratedAt}} · автономный HTML · узлы {{.Influence.ShownNodes}} · связи {{.Influence.ShownEdges}}</div>
    </div>
    <div class="hero-side">
      <div class="env-card">
        <div class="env-title">Покрытие графа</div>
        <strong class="env-device">{{if .Influence.HasClassGraph}}сигналы выполнения + статический граф{{else}}только сигналы выполнения{{end}}</strong>
        <div class="env-subtitle">{{.Influence.StandaloneReason}}</div>
        <div class="env-grid">
          <div class="env-item"><div class="env-label">Узлы выполнения</div><div class="env-value">{{.Influence.RuntimeNodes}}</div><div class="env-detail">{{.Influence.RuntimeEdges}} связей выполнения</div></div>
          <div class="env-item"><div class="env-label">Статика</div><div class="env-value">{{.Influence.StaticNodes}}</div><div class="env-detail">{{.Influence.StaticEdges}} связей</div></div>
        </div>
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#graph">Граф</a>
  <a href="#nodes">Узлы</a>
  <a href="#edges">Связи</a>
  <a href="#heuristic">Итог</a>
</nav>
<main>
  <section id="graph" class="panel">
    <div class="panel-head">
      <div>
        <h2>Карта влияния</h2>
        <div class="panel-kicker">Размер узла отражает оценку. Сплошные связи подтверждены сигналами выполнения, пунктирные/тусклые узлы пока видны только по статическому графу.</div>
      </div>
      <span class="pill">код → симптомы</span>
    </div>
    {{influenceGraphSVG .Influence}}
  </section>

  <section id="nodes" class="panel">
    <div class="panel-head">
      <div><h2>Проблемные классы</h2><div class="panel-kicker">Классы отсортированы по суммарному влиянию на сеть, UI, главный поток, память, лог-спам и флоу.</div></div>
    </div>
    <details class="fold influence-table-fold">
      <summary><span>Показать проблемные классы</span><span class="method-kind">{{len .Influence.TopNodes}} классов</span></summary>
      <div class="fold-body">
        <table class="influence-table influence-node-table">
          <tr><th>Класс</th><th>{{tip "Оценка" (scoreHelp "influence")}}</th><th>Статус</th><th>Метрики</th><th>Причины</th><th>Флоу / экраны</th></tr>
          {{range .Influence.TopNodes}}
          <tr>
            <td><code>{{.ClassName}}</code></td>
            <td><div class="table-stack"><strong>{{printf "%.1f" .Score}}</strong><span>{{influenceSeverity .Severity}}</span></div></td>
            <td>{{influenceStatus .Status}}</td>
            <td>
              <div class="table-metrics">
                <div class="table-metric">Проблемы<strong>{{.Problems}}</strong></div>
                <div class="table-metric">Спам логами<strong>{{.LogSpam}}</strong></div>
                <div class="table-metric">Главный поток<strong>{{.MainThreadMS}} мс</strong></div>
                <div class="table-metric">Сеть<strong>{{.NetworkMS}} мс</strong></div>
                <div class="table-metric">UI-подторм.<strong>{{.UIJank}}</strong></div>
                <div class="table-metric">Удержано<strong>{{.Retained}}</strong></div>
              </div>
            </td>
            <td>{{join .Reasons ", "}}</td>
            <td>{{join .Flows ", "}} {{join .Screens ", "}}</td>
          </tr>
          {{else}}<tr><td colspan="6" class="muted">Нет узлов влияния.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="edges" class="panel">
    <div class="panel-head">
      <div><h2>Связи влияния</h2><div class="panel-kicker">Связи строятся из статического ASM-графа и усиливаются, если один из классов проявился в симптомах выполнения.</div></div>
    </div>
    <details class="fold influence-table-fold">
      <summary><span>Показать связи влияния</span><span class="method-kind">{{len .Influence.TopEdges}} связей</span></summary>
      <div class="fold-body">
        <table class="influence-table influence-edge-table">
          <tr><th>Откуда</th><th>Куда</th><th>Вызовы</th><th>Вес</th><th>Выполнение</th><th>Пояснение</th></tr>
          {{range .Influence.TopEdges}}
          <tr><td><code>{{.From}}</code></td><td><code>{{.To}}</code></td><td>{{.Count}}</td><td>{{printf "%.1f" .Influence}}</td><td>{{if .RuntimeConfirmed}}да{{else}}нет{{end}}</td><td>{{.Reason}}</td></tr>
          {{else}}<tr><td colspan="6" class="muted">Нет статических связей. Передайте ` + "`--class-graph`" + `, чтобы увидеть ребра между классами.</td></tr>{{end}}
        </table>
      </div>
    </details>
  </section>

  <section id="heuristic" class="panel">
    <div class="panel-head">
      <div><h2>Эвристика</h2><div class="panel-kicker">Короткий вывод по графу влияния. Это не заменяет профилировщик, но помогает выбрать первую точку расследования.</div></div>
    </div>
    <div class="heuristic-grid">
      {{range .Influence.Heuristic}}
      <div class="heuristic-card {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">Нет эвристических выводов.</div>{{end}}
    </div>
    <p class="help-text">Статический узел без доказательств выполнения означает “код связан, но в этом прогоне не проявился”: например фича могла быть выключена флагом или сценарий не дошел до этого класса.</p>
  </section>
</main>
<script>` + reportJS + `
(() => {
  const escapeHTML = (value) => String(value).replace(/[&<>"']/g, (char) => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  }[char]));

  document.querySelectorAll('.influence-graph-card').forEach((card) => {
    const selection = card.querySelector('[data-influence-selection]');
    const graph = card.querySelector('.influence-graph');
    const nodes = Array.from(card.querySelectorAll('.influence-node[data-node]'));
    const edges = Array.from(card.querySelectorAll('.influence-edge[data-from][data-to]'));
    if (!graph || nodes.length === 0) return;

    const modeButtons = Array.from(card.querySelectorAll('[data-influence-mode]'));
    const resetButton = card.querySelector('[data-influence-reset]');
    const outgoing = new Map();
    const incoming = new Map();
    edges.forEach((edge, index) => {
      const from = edge.dataset.from || '';
      const to = edge.dataset.to || '';
      if (!from || !to) return;
      if (!outgoing.has(from)) outgoing.set(from, []);
      if (!incoming.has(to)) incoming.set(to, []);
      outgoing.get(from).push({ index, to });
      incoming.get(to).push({ index, from });
    });

    let pinnedNode = '';
    let currentNode = '';
    let mode = 'paths';

    const walkPathsFrom = (start) => {
      const reached = new Set([start]);
      const activeEdges = new Set();
      const stack = [start];
      while (stack.length > 0) {
        const current = stack.pop();
        (outgoing.get(current) || []).forEach(({ index, to }) => {
          activeEdges.add(index);
          if (!reached.has(to)) {
            reached.add(to);
            stack.push(to);
          }
        });
      }
      return { reached, activeEdges };
    };

    const walkTreeFrom = (start) => {
      const reached = new Set([start]);
      const activeEdges = new Set();
      const queue = [start];
      while (queue.length > 0) {
        const current = queue.shift();
        (outgoing.get(current) || []).forEach(({ index, to }) => {
          if (reached.has(to)) return;
          reached.add(to);
          activeEdges.add(index);
          queue.push(to);
        });
      }
      return { reached, activeEdges };
    };

    const incidentFrom = (start) => {
      const reached = new Set([start]);
      const activeEdges = new Set();
      (outgoing.get(start) || []).forEach(({ index, to }) => {
        activeEdges.add(index);
        reached.add(to);
      });
      (incoming.get(start) || []).forEach(({ index, from }) => {
        activeEdges.add(index);
        reached.add(from);
      });
      return { reached, activeEdges };
    };

    const analysisFor = (start) => {
      if (mode === 'tree') return walkTreeFrom(start);
      if (mode === 'node') return incidentFrom(start);
      return walkPathsFrom(start);
    };

    const modeText = () => {
      if (mode === 'tree') return 'остов влияния';
      if (mode === 'node') return 'ближайшие связи вершины';
      return 'исходящие пути';
    };

    const syncModeButtons = () => {
      modeButtons.forEach((button) => {
        button.classList.toggle('is-active', button.dataset.influenceMode === mode);
      });
    };

    const reset = () => {
      pinnedNode = '';
      currentNode = '';
      nodes.forEach((node) => node.classList.remove('is-selected', 'is-related', 'is-neighbor', 'is-dimmed'));
      edges.forEach((edge) => edge.classList.remove('is-path', 'is-tree', 'is-dimmed'));
      if (selection) {
        selection.textContent = 'Выберите режим и наведите мышью на вершину: можно выделить вершину, исходящие пути или остов влияния.';
      }
    };

    const selectNode = (name) => {
      currentNode = name;
      const { reached, activeEdges } = analysisFor(name);
      nodes.forEach((node) => {
        const nodeName = node.dataset.node || '';
        const selected = nodeName === name;
        const related = reached.has(nodeName);
        node.classList.toggle('is-selected', selected);
        node.classList.toggle('is-related', related && !selected);
        node.classList.toggle('is-neighbor', mode === 'node' && related && !selected);
        node.classList.toggle('is-dimmed', !related);
      });
      edges.forEach((edge, index) => {
        const active = activeEdges.has(index);
        edge.classList.toggle('is-path', active);
        edge.classList.toggle('is-tree', active && mode === 'tree');
        edge.classList.toggle('is-dimmed', !active);
      });
      if (selection) {
        selection.innerHTML = '<strong>' + escapeHTML(name) + '</strong>: выделены ' + modeText() + ', ' +
          reached.size + ' узлов и ' + activeEdges.size + ' связей.';
      }
    };

    modeButtons.forEach((button) => {
      button.addEventListener('click', (event) => {
        event.stopPropagation();
        mode = button.dataset.influenceMode || 'paths';
        syncModeButtons();
        if (currentNode) selectNode(currentNode);
      });
    });
    if (resetButton) {
      resetButton.addEventListener('click', (event) => {
        event.stopPropagation();
        reset();
      });
    }
    syncModeButtons();

    nodes.forEach((node) => {
      const name = node.dataset.node || '';
      node.addEventListener('pointerenter', () => {
        if (!pinnedNode) selectNode(name);
      });
      node.addEventListener('focus', () => selectNode(name));
      node.addEventListener('click', (event) => {
        event.stopPropagation();
        pinnedNode = name;
        selectNode(name);
      });
      node.addEventListener('keydown', (event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          pinnedNode = name;
          selectNode(name);
        }
      });
    });

    card.addEventListener('pointerleave', () => {
      if (!pinnedNode) reset();
    });
    graph.addEventListener('click', () => reset());
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Escape' && pinnedNode) reset();
    });
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
<body{{if .PresentationMode}} class="presentation-page"{{end}}>
<header class="hero">
  <div class="report-hero-grid">
    <div class="report-hero-title">
      <div class="eyebrow">Jank Hunter · обзор</div>
      <h1>Отчет по сигналам выполнения</h1>
      <div class="subhead">{{.Summary.Title}} · создан {{.GeneratedAt}} · автономный HTML</div>
    </div>
    <div class="report-hero-lower">
      <div class="report-hero-context">
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
      </div>
      <div class="report-hero-control">
      <div class="hero-meta">
        <div class="chip">Логи <strong>{{.Summary.LogCount}}</strong></div>
        <div class="chip">События <strong>{{.Summary.EventCount}}</strong></div>
        <div class="chip">Длительность <strong>{{humanDuration .Summary.DurationMS}}</strong></div>
      </div>
      {{if or .MathReportHref .InfluenceReportHref}}<div class="hero-actions">{{if .MathReportHref}}<a class="math-link" href="{{.MathReportHref}}">λ Анализ</a>{{end}}{{if .InfluenceReportHref}}<a class="math-link" href="{{.InfluenceReportHref}}">Граф влияния</a>{{end}}</div>{{end}}
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#overview">Обзор</a>
  <a href="#network">Сеть</a>
  <a href="#ui">UI</a>
  <a href="#flows">Флоу</a>
  <a href="#code-problems">Реестр кода</a>
  <a href="#owners">Источники</a>
  <a href="#memory-leaks">Утечки</a>
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
    <div class="report-guide">
      <div class="guide-card"><strong>Как читать отчет</strong>Сначала смотрите общий статус и красные/желтые находки, затем открывайте разделы флоу, источников и граф влияния. Так проще перейти от симптома к коду.</div>
      <div class="guide-card"><strong>Что важно</strong>Один показатель редко объясняет проблему. Ищите совпадение во времени: UI-подтормаживания, паузы главного потока, сетевые хвосты, память и спам логами.</div>
      <div class="guide-card"><strong>Что исправлять</strong>Начинайте с источников с высоким вкладом и понятным контекстом: экран, флоу, шаг, маршрут или класс. Неочевидные пики лучше подтвердить профилировщиком.</div>
    </div>
    <h3>Индикаторы здоровья</h3>
    <div class="ring-row">
      <div class="gauge-card"><div class="gauge" style="{{ringStyle .Summary.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIJankPct}}%</strong><span>UI</span></div></div></div><div class="gauge-label">UI&#8209;подтормаживания</div></div>
      <div class="gauge-card"><div class="gauge" style="{{ringStyle (rate .Summary.HTTPFailed .Summary.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Summary.HTTPFailed .Summary.HTTPCount)}}%</strong><span>HTTP</span></div></div></div><div class="gauge-label">HTTP-ошибки</div></div>
      <div class="gauge-card"><div class="gauge" style="{{ringStyle (fpsScore .Summary.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Summary.UIAvgFPS}}</strong><span>FPS</span></div></div></div><div class="gauge-label">Средний FPS</div></div>
    </div>
    {{if .Summary.Influence.Available}}
    <h3>Граф влияния кода</h3>
    <p class="help-text">Короткий срез “злых” узлов: классы получают вес по паузам главного потока, сети, памяти, лог-спаму, проблемным окнам и флоу. Подробная карта связей вынесена в отдельный отчет.</p>
    <div class="grid influence-grid">
      {{range topInfluenceNodes .Summary.Influence 6}}
      <div class="metric influence-tile" data-tip="{{.ClassName}} · {{join .Reasons ", "}}">
        <div class="label">{{influenceSeverity .Severity}}</div>
        <div class="value">{{printf "%.1f" .Score}}</div>
        <div class="influence-tile-body" tabindex="0">
          <div class="hint"><code>{{.ClassName}}</code></div>
          <div class="hint influence-tile-reasons">{{join .Reasons ", "}}</div>
        </div>
      </div>
      {{end}}
    </div>
    {{if .InfluenceReportHref}}<p class="help-text"><a href="{{.InfluenceReportHref}}">Открыть подробный граф влияния кода</a></p>{{end}}
    {{end}}
  </section>

  <section id="code-problems" class="panel">
    <div class="panel-head">
      <div>
        <h2>Реестр проблем кода</h2>
        <div class="panel-kicker">Ранжированный список классов и методов, где сошлись риск АНР, сеть, память, UI, спам логами, удержания и граф влияния.</div>
      </div>
      <span class="pill">сортировка и фильтры</span>
    </div>
    <p class="help-text">Оценка — это приоритет расследования внутри текущего прогона, а не абсолютная оценка качества кода. Чем выше число, тем раньше стоит открыть строку и проверить доказательства, контекст и рекомендацию.</p>
    {{scoreGuide "code"}}
    {{with codeProblemCategories .Summary.CodeProblems}}
    <div class="registry-insights"><span class="registry-insights-label">Категории</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-category="{{.Name}}">{{.Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
    {{end}}
    {{with codeProblemSeverities .Summary.CodeProblems}}
    <div class="registry-insights"><span class="registry-insights-label">Риск</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-severity="{{.Name}}">{{severityLabel .Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
    {{end}}
    <div class="code-registry" data-code-registry>
      <div class="registry-toolbar">
        <input type="search" data-code-registry-search placeholder="Фильтр по классу, методу, экрану, флоу, маршруту или проблеме" aria-label="Фильтр реестра проблем кода">
        <select data-code-registry-severity aria-label="Фильтр по риску">
          <option value="">Все уровни риска</option>
          <option value="high">Критично</option>
          <option value="medium">Предупреждение</option>
          <option value="ok">Низкий риск</option>
        </select>
        <select data-code-registry-category aria-label="Фильтр по категории">
          <option value="">Все категории</option>
          <option value="Сеть">Сеть</option>
          <option value="UI">UI</option>
          <option value="Главный поток">Главный поток</option>
          <option value="Память">Память</option>
          <option value="Логи">Логи</option>
          <option value="Выполнение">Выполнение</option>
          <option value="Граф влияния">Граф влияния</option>
          <option value="ANR-risk">ANR-risk</option>
          <option value="OOM-risk">OOM-risk</option>
          <option value="GC pressure">GC pressure</option>
          <option value="duplicate network">duplicate network</option>
          <option value="lifecycle leak">lifecycle leak</option>
          <option value="log spam">log spam</option>
          <option value="main-thread IO">main-thread IO</option>
        </select>
        <span class="registry-counter" data-code-registry-count></span>
      </div>
      <div class="problem-empty">По текущим фильтрам проблемных классов не найдено.</div>
      <table class="code-problem-table">
        <thead>
          <tr>
            <th><button type="button" data-code-sort="score">Оценка</button></th>
            <th><button type="button" data-code-sort="class">Класс / метод</button></th>
            <th><button type="button" data-code-sort="category">Категории</button></th>
            <th>Подробности</th>
          </tr>
        </thead>
        <tbody>
          {{range .Summary.CodeProblems}}
          <tr data-code-problem-row data-score="{{printf "%.1f" .Score}}" data-severity="{{.Severity}}" data-class="{{codeProblemLocation .}}" data-categories="{{join .Categories "|"}}" data-search="{{codeProblemSearchText .}}">
            <td><span class="problem-score {{severityClass .Severity}}">{{printf "%.1f" .Score}}</span><div class="muted">{{severityLabel .Severity}}</div>{{if .RuntimeEvidence}}<div class="muted">есть выполнение</div>{{else}}<div class="muted">статический след</div>{{end}}</td>
            <td><div class="problem-location"><code>{{.ClassName}}</code>{{if .Method}}<div class="method"><code>{{.Method}}</code></div>{{end}}</div></td>
            <td><div class="problem-tags">{{range .Categories}}<span class="problem-chip">{{.}}</span>{{end}}</div><div class="muted">{{join .Problems ", "}}</div></td>
            <td>
              <details class="code-problem-details">
                <summary><span class="code-problem-summary-main"><strong>Доказательства и рекомендация</strong><em>{{.Evidence}}</em></span><small>{{len .Signals}} сигналов{{if .DrillDown}} · {{len .DrillDown}} флоу{{end}}</small></summary>
                <div class="code-problem-detail-body">
                  <div class="code-problem-detail-block span-all"><strong>Доказательство</strong><p>{{.Evidence}}</p></div>
                  <div class="code-problem-detail-block"><strong>Контекст</strong><div class="problem-context">{{range .Screens}}<div>экран <code>{{.}}</code></div>{{end}}{{range .Flows}}<div>флоу <code>{{.}}</code></div>{{end}}{{range .Steps}}<div>шаг <code>{{.}}</code></div>{{end}}{{range .Routes}}<div>маршрут <code>{{.}}</code></div>{{end}}</div></div>
                  <div class="code-problem-detail-block"><strong>Влияние</strong><p>{{.Impact}}</p></div>
                  <div class="code-problem-detail-block span-all"><strong>Что проверить</strong><p>{{.Recommendation}}</p></div>
                  {{if .DrillDown}}<div class="code-problem-detail-block span-all"><strong>Drill-down</strong><div class="problem-drilldown">{{range .DrillDown}}<div class="problem-drill"><strong>{{codeProblemDrillPath .}}</strong><span>Доказательство: {{.Evidence}}</span><span>Рекомендация: {{.Recommendation}}</span></div>{{end}}</div></div>{{end}}
                  <div class="code-problem-detail-block span-all"><strong>Сигналы</strong><div class="problem-signals">{{range .Signals}}<div class="problem-signal {{severityClass .Severity}}"><strong>{{.Name}}</strong><small>{{.Category}} · {{codeProblemMetric .}}<br>{{.Detail}}</small></div>{{end}}</div></div>
                </div>
              </details>
            </td>
          </tr>
          {{else}}<tr><td colspan="4" class="muted">Реестр проблем кода пуст: текущий прогон не дал привязанных к классу сигналов. Проверьте ASM-опции owners, flowInteractions, runtimeCallGraph и logSpam.</td></tr>{{end}}
        </tbody>
      </table>
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
        <h3>Вызовы выполнения</h3>
        <table>
          <tr><th>Экран</th><th>Флоу</th><th>Шаг</th><th>Откуда</th><th>Куда</th><th>Количество</th><th>Итого</th><th>Макс.</th></tr>
          {{range .Summary.RuntimeCalls}}
          <tr><td><code>{{.Screen}}</code></td><td><code>{{.Flow}}</code></td><td><code>{{.Step}}</code></td><td><code>{{.Caller}}</code></td><td><code>{{.Callee}}</code></td><td>{{.Count}}</td><td>{{.TotalMS}} мс</td><td>{{.MaxMS}} мс</td></tr>
          {{else}}<tr><td colspan="8" class="muted">Нет графа вызовов выполнения. Включите ASM-опцию runtimeCallGraph для целевых пакетов.</td></tr>{{end}}
        </table>
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

  <section id="memory-leaks" class="panel">
    <div class="panel-head">
      <div>
        <h2>Разбор утечек памяти</h2>
        <div class="panel-kicker">Пользовательские классы, Activity/Fragment/Context и вероятные держатели, которые не отпустили объект после задержки проверки удержания.</div>
      </div>
      <span class="pill">удержания</span>
    </div>
    <p class="help-text">Этот блок не показывает системный шум сам по себе: системные классы важны, когда рядом есть пользовательский держатель или экран. Точный путь до корня GC без дампа памяти недоступен, поэтому держатель здесь — вероятный владелец из контекста выполнения или места вызова watch-метода.</p>
    <div class="leak-limitations">Если в колонке “держатель” написано “не определен”, добавьте <code>ownerHint</code> в <code>JankHunter.watchObject(...)</code> или оберните подозрительный участок в <code>JankHunter.withOwner(...)</code>. Тогда следующий прогон покажет, какой пользовательский класс вероятнее всего удерживает объект.</div>
    {{scoreGuide "leak"}}
    <div class="code-registry" data-code-registry>
      <div class="registry-toolbar">
        <input type="search" data-code-registry-search placeholder="Фильтр по классу, держателю, экрану, флоу или рекомендации" aria-label="Фильтр реестра утечек памяти">
        <select data-code-registry-severity aria-label="Фильтр утечек по риску">
          <option value="">Все уровни риска</option>
          <option value="high">Критично</option>
          <option value="medium">Предупреждение</option>
          <option value="ok">Норма</option>
        </select>
        <select data-code-registry-category aria-label="Фильтр утечек по типу объекта">
          <option value="">Все типы</option>
          <option value="экран / Activity">Activity</option>
          <option value="Fragment">Fragment</option>
          <option value="Context">Context</option>
          <option value="View / binding">View / binding</option>
          <option value="ресурс">Ресурс</option>
          <option value="системный объект">Системный объект</option>
          <option value="пользовательский объект">Пользовательский объект</option>
        </select>
        <span class="registry-counter" data-code-registry-count></span>
      </div>
      <div class="problem-empty">По текущим фильтрам подозрений утечек не найдено.</div>
      <table class="code-problem-table leak-table">
        <thead>
          <tr>
            <th><button type="button" data-code-sort="severity">Риск</button></th>
            <th><button type="button" data-code-sort="class">Удержанный объект</button></th>
            <th><button type="button" data-code-sort="category">Тип</button></th>
            <th>Вероятный держатель</th>
            <th>Контекст</th>
            <th><button type="button" data-code-sort="score">Оценка</button></th>
            <th>{{tip "Оценка удержанного размера" "Ориентировочная оценка удержанного размера. Без дампа памяти это не точный размер удержанной кучи, а расчет по типу объекта, числу удержаний, возрасту и PSS процесса."}}</th>
            <th>{{tip "Мини-дерево доминирования" "Вероятная цепочка владения из контекста выполнения: экран, флоу, шаг, держатель и удержанный объект. Точный корень GC требует дампа памяти."}}</th>
            <th>Влияние</th>
            <th>Что проверить</th>
            <th>Доказательства</th>
          </tr>
        </thead>
        <tbody>
          {{range .Summary.MemoryLeaks}}
          <tr data-code-problem-row data-score="{{printf "%.1f" .Score}}" data-severity="{{.Severity}}" data-class="{{.ClassName}}" data-categories="{{.ObjectKind}}" data-search="{{memoryLeakSearchText .}}">
            <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span></td>
            <td><div class="problem-location"><code>{{.ClassName}}</code></div></td>
            <td><div class="leak-object-kind">{{.ObjectKind}}</div>{{if .SystemRetained}}<div class="muted">системный объект</div>{{else if .UserOwned}}<div class="muted">пользовательский код</div>{{end}}</td>
            <td><code>{{.Holder}}</code><div class="leak-holder-quality">{{.HolderQuality}}</div></td>
            <td><div class="problem-context">{{if .Screen}}<div>экран <code>{{.Screen}}</code></div>{{end}}{{if .Flow}}<div>флоу <code>{{.Flow}}</code></div>{{end}}{{if .Step}}<div>шаг <code>{{.Step}}</code></div>{{end}}</div></td>
            <td><div>{{printf "%.1f" .Score}}</div><div class="muted">{{.Count}} шт · {{humanDuration .MaxAgeMS}}</div></td>
            <td><div>{{dataSize .EstimatedRetainedKB}}</div><div class="muted">{{.RetainedSizeConfidence}}</div><div class="muted">{{.RetainedSizeExplanation}}</div></td>
            <td><div class="leak-dominator">{{range .DominatorPath}}<span>{{.}}</span>{{end}}</div><div class="muted">{{.DominatorTreeExplanation}}</div><div class="leak-chain-summary">{{.LeakChainSummary}}</div></td>
            <td>{{.Impact}}</td>
            <td>{{.Recommendation}}<div class="leak-chain-actions"><strong>Быстрые проверки цепочки</strong>{{range .LeakChainActions}}<span>{{.}}</span>{{end}}</div></td>
            <td>{{.Evidence}}</td>
          </tr>
          {{else}}<tr><td colspan="11" class="muted">Подозрений на утечки памяти нет. Если вы ожидаете увидеть Activity/Fragment, проверьте, что проверка удержания объектов включена и объект был передан в watchObject/watchActivity.</td></tr>{{end}}
        </tbody>
      </table>
    </div>
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
      {{range significantReportFindings .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">Нет значимых эвристических предупреждений.</div>{{end}}
    </div>
    <h3>Рекомендации</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>Нет дополнительных рекомендаций.</li>{{end}}
    </ul>
  </section>
</main>
<script>` + reportJS + `</script>
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
<body{{if .PresentationMode}} class="presentation-page"{{end}}>
<header class="hero">
  <div class="report-hero-grid">
    <div class="report-hero-title">
      <div class="eyebrow">Jank Hunter · сравнение</div>
      <h1>Панель контроля регрессий</h1>
      <div class="subhead">создан {{.GeneratedAt}} · база против кандидата · автономный HTML</div>
    </div>
    <div class="report-hero-lower">
      <div class="report-hero-context">
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
      </div>
      <div class="report-hero-control">
      <div class="hero-meta">
        <div class="chip">Логи базы <strong>{{.Comparison.Baseline.LogCount}}</strong></div>
        <div class="chip">Логи кандидата <strong>{{.Comparison.Candidate.LogCount}}</strong></div>
        <div class="chip">Дельты <strong>{{len .Comparison.Deltas}}</strong></div>
      </div>
      {{if or .MathReportHref .InfluenceReportHref}}<div class="hero-actions">{{if .MathReportHref}}<a class="math-link" href="{{.MathReportHref}}">λ Анализ</a>{{end}}{{if .InfluenceReportHref}}<a class="math-link" href="{{.InfluenceReportHref}}">Граф влияния</a>{{end}}</div>{{end}}
      </div>
    </div>
  </div>
</header>
<nav class="nav">
  <a href="#compare">Сравнение</a>
  <a href="#code-problems">Реестр кода</a>
  <a href="#memory-leaks">Утечки</a>
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
        <div class="compare-pair-title">{{tip "Главный поток" "Самая длинная пауза главного потока. При 2 мс watchdog задержек такие пики особенно важно смотреть рядом с владельцами работ."}}</div>
        <div class="compare-values"><div class="compare-value"><span>База</span><strong>{{.Comparison.Baseline.StallMaxMS}} мс</strong></div><div class="compare-value"><span>Кандидат</span><strong>{{.Comparison.Candidate.StallMaxMS}} мс</strong></div></div>
        <div class="compare-delta">События пауз {{.Comparison.Baseline.StallCount}} → {{.Comparison.Candidate.StallCount}}</div>
      </div>
      <div class="compare-pair">
        <div class="compare-pair-title">{{tip "Флоу и причины" "Сумма агрегированных проблемных окон и спама логами по кортежам контекста флоу. Рост показывает, что кандидат чаще попадает в объяснимые проблемные участки сценария."}}</div>
        <div class="compare-values"><div class="compare-value"><span>Проблемы</span><strong>{{summaryProblems .Comparison.Baseline}} → {{summaryProblems .Comparison.Candidate}}</strong></div><div class="compare-value"><span>Спам логами</span><strong>{{summaryLogSpam .Comparison.Baseline}} → {{summaryLogSpam .Comparison.Candidate}}</strong></div></div>
        <div class="compare-delta">Флоу {{len .Comparison.Baseline.Flows}} → {{len .Comparison.Candidate.Flows}}</div>
      </div>
    </div>
    <div class="report-guide">
      <div class="guide-card"><strong>Как читать сравнение</strong>Рост задержек, ошибок, памяти, подтормаживаний и спама логами обычно означает ухудшение кандидата. Доверие и выборка показывают, насколько этому можно верить.</div>
      <div class="guide-card"><strong>Где искать причину</strong>После матрицы регрессий открывайте “Где изменилось” и “Флоу”: там видно конкретный маршрут, экран или источник, который изменился.</div>
      <div class="guide-card"><strong>Когда эскалировать</strong>Критичные регрессии с повторяемостью, сетевые циклы или рост интегральной нагрузки лучше сразу превращать в задачу на исправление.</div>
    </div>
    {{scoreGuide "compare"}}
    <h3>Кольцевые индикаторы</h3>
    <div class="ring-row">
      <div class="gauge-card"><div class="gauge" style="{{ringStyle .Comparison.Candidate.UIJankPct}}; --color: var(--warn)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIJankPct}}%</strong><span>UI</span></div></div></div><div class="gauge-label">Подтормаживания кандидата</div></div>
      <div class="gauge-card"><div class="gauge" style="{{ringStyle (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}; --color: var(--bad)"><div class="gauge-core"><div><strong>{{printf "%.1f" (rate .Comparison.Candidate.HTTPFailed .Comparison.Candidate.HTTPCount)}}%</strong><span>HTTP</span></div></div></div><div class="gauge-label">Ошибки кандидата</div></div>
      <div class="gauge-card"><div class="gauge" style="{{ringStyle (fpsScore .Comparison.Candidate.UIAvgFPS)}}; --color: var(--ok)"><div class="gauge-core"><div><strong>{{printf "%.1f" .Comparison.Candidate.UIAvgFPS}}</strong><span>FPS</span></div></div></div><div class="gauge-label">FPS кандидата</div></div>
    </div>
    {{if .Comparison.Candidate.Influence.Available}}
    <h3>Граф влияния кандидата</h3>
    <table>
      <tr><th>Класс</th><th>{{tip "Оценка" (scoreHelp "influence")}}</th><th>Риск</th><th>Причины</th></tr>
      {{range topInfluenceNodes .Comparison.Candidate.Influence 6}}
      <tr><td><code>{{.ClassName}}</code></td><td>{{printf "%.1f" .Score}}</td><td>{{influenceSeverity .Severity}}</td><td>{{join .Reasons ", "}}</td></tr>
      {{end}}
    </table>
    {{if .InfluenceReportHref}}<p class="help-text"><a href="{{.InfluenceReportHref}}">Открыть подробный граф влияния кандидата</a></p>{{end}}
    {{end}}
  </section>

  <section id="code-problems" class="panel">
    <div class="panel-head">
      <div>
        <h2>Реестр проблем кода кандидата</h2>
        <div class="panel-kicker">Сравнение проблемных классов и методов кандидата с базой: новые точки, усиление оценки и текущие доказательства.</div>
      </div>
      <span class="pill">кандидат против базы</span>
    </div>
    <p class="help-text">Дельта оценки показывает, насколько сильнее или слабее стало проблемное место у кандидата. Положительная дельта — повод проверить строку раньше.</p>
    {{scoreGuide "code"}}
    {{with codeProblemCategories .Comparison.Candidate.CodeProblems}}
    <div class="registry-insights"><span class="registry-insights-label">Категории</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-category="{{.Name}}">{{.Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
    {{end}}
    {{with codeProblemSeverities .Comparison.Candidate.CodeProblems}}
    <div class="registry-insights"><span class="registry-insights-label">Риск</span>{{range .}}<button type="button" class="registry-chip {{severityClass .Severity}}" data-registry-severity="{{.Name}}">{{severityLabel .Name}} <strong>{{.Count}}</strong></button>{{end}}</div>
    {{end}}
    <div class="code-registry" data-code-registry>
      <div class="registry-toolbar">
        <input type="search" data-code-registry-search placeholder="Фильтр по классу, методу, экрану, флоу, маршруту или проблеме" aria-label="Фильтр сравнительного реестра проблем кода">
        <select data-code-registry-severity aria-label="Фильтр по риску">
          <option value="">Все уровни риска</option>
          <option value="high">Критично</option>
          <option value="medium">Предупреждение</option>
          <option value="ok">Норма</option>
        </select>
        <select data-code-registry-category aria-label="Фильтр по категории">
          <option value="">Все категории</option>
          <option value="Сеть">Сеть</option>
          <option value="UI">UI</option>
          <option value="Главный поток">Главный поток</option>
          <option value="Память">Память</option>
          <option value="Логи">Логи</option>
          <option value="Выполнение">Выполнение</option>
          <option value="Граф влияния">Граф влияния</option>
          <option value="ANR-risk">ANR-risk</option>
          <option value="OOM-risk">OOM-risk</option>
          <option value="GC pressure">GC pressure</option>
          <option value="duplicate network">duplicate network</option>
          <option value="lifecycle leak">lifecycle leak</option>
          <option value="log spam">log spam</option>
          <option value="main-thread IO">main-thread IO</option>
        </select>
        <span class="registry-counter" data-code-registry-count></span>
      </div>
      <div class="problem-empty">По текущим фильтрам проблемных классов не найдено.</div>
      <table class="code-problem-table">
        <thead>
          <tr>
            <th><button type="button" data-code-sort="severity">Статус</button></th>
            <th><button type="button" data-code-sort="class">Класс / метод</button></th>
            <th><button type="button" data-code-sort="score">Оценка</button></th>
            <th><button type="button" data-code-sort="category">Категории</button></th>
            <th>Подробности кандидата</th>
          </tr>
        </thead>
        <tbody>
          {{range codeProblemCompareRows .Comparison}}
          <tr data-code-problem-row data-score="{{printf "%.1f" .DeltaScore}}" data-severity="{{.Severity}}" data-class="{{codeProblemLocation .Candidate}}" data-categories="{{join .Candidate.Categories "|"}}" data-search="{{codeProblemSearchText .Candidate}} {{.Status}}">
            <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span><div class="muted">{{.Status}}</div></td>
            <td><div class="problem-location"><code>{{.Candidate.ClassName}}</code>{{if .Candidate.Method}}<div class="method"><code>{{.Candidate.Method}}</code></div>{{end}}</div></td>
            <td><div>база {{printf "%.1f" .BaselineScore}}</div><div>кандидат {{printf "%.1f" .Candidate.Score}}</div><div class="muted">дельта {{printf "%+.1f" .DeltaScore}}</div></td>
            <td><div class="problem-tags">{{range .Candidate.Categories}}<span class="problem-chip">{{.}}</span>{{end}}</div><div class="muted">{{join .Candidate.Problems ", "}}</div></td>
            <td>
              <details class="code-problem-details">
                <summary><span class="code-problem-summary-main"><strong>Доказательства и рекомендация</strong><em>{{.Candidate.Evidence}}</em></span><small>{{len .Candidate.Signals}} сигналов{{if .Candidate.DrillDown}} · {{len .Candidate.DrillDown}} флоу{{end}}</small></summary>
                <div class="code-problem-detail-body">
                  <div class="code-problem-detail-block span-all"><strong>Доказательство</strong><p>{{.Candidate.Evidence}}</p></div>
                  <div class="code-problem-detail-block"><strong>Контекст</strong><div class="problem-context">{{range .Candidate.Screens}}<div>экран <code>{{.}}</code></div>{{end}}{{range .Candidate.Flows}}<div>флоу <code>{{.}}</code></div>{{end}}{{range .Candidate.Steps}}<div>шаг <code>{{.}}</code></div>{{end}}{{range .Candidate.Routes}}<div>маршрут <code>{{.}}</code></div>{{end}}</div></div>
                  <div class="code-problem-detail-block"><strong>Влияние кандидата</strong><p>{{.Candidate.Impact}}</p></div>
                  <div class="code-problem-detail-block span-all"><strong>Что проверить</strong><p>{{.Candidate.Recommendation}}</p></div>
                  {{if .Candidate.DrillDown}}<div class="code-problem-detail-block span-all"><strong>Drill-down</strong><div class="problem-drilldown">{{range .Candidate.DrillDown}}<div class="problem-drill"><strong>{{codeProblemDrillPath .}}</strong><span>Доказательство: {{.Evidence}}</span><span>Рекомендация: {{.Recommendation}}</span></div>{{end}}</div></div>{{end}}
                  <div class="code-problem-detail-block span-all"><strong>Сигналы кандидата</strong><div class="problem-signals">{{range .Candidate.Signals}}<div class="problem-signal {{severityClass .Severity}}"><strong>{{.Name}}</strong><small>{{.Category}} · {{codeProblemMetric .}}<br>{{.Detail}}</small></div>{{end}}</div></div>
                </div>
              </details>
            </td>
          </tr>
          {{else}}<tr><td colspan="5" class="muted">Реестр проблем кода кандидата пуст: в сравнении нет привязанных к классу сигналов.</td></tr>{{end}}
        </tbody>
      </table>
    </div>
  </section>

  <section id="memory-leaks" class="panel">
    <div class="panel-head">
      <div>
        <h2>Сравнение утечек памяти</h2>
        <div class="panel-kicker">Новые и усилившиеся удержания кандидата: объект, вероятный держатель, контекст и изменение относительно базы.</div>
      </div>
      <span class="pill">дельта удержаний</span>
    </div>
    <p class="help-text">Сравнение смотрит не только количество, но и возраст удержания. Если держатель не определен, добавьте <code>ownerHint</code> или <code>withOwner</code>, чтобы следующий прогон показал пользовательский класс точнее.</p>
    {{scoreGuide "leak"}}
    <div class="code-registry" data-code-registry>
      <div class="registry-toolbar">
        <input type="search" data-code-registry-search placeholder="Фильтр по удержанному объекту, держателю, экрану или рекомендации" aria-label="Фильтр сравнительного реестра утечек памяти">
        <select data-code-registry-severity aria-label="Фильтр утечек по риску">
          <option value="">Все уровни риска</option>
          <option value="high">Критично</option>
          <option value="medium">Предупреждение</option>
          <option value="ok">Норма</option>
        </select>
        <select data-code-registry-category aria-label="Фильтр утечек по типу объекта">
          <option value="">Все типы</option>
          <option value="экран / Activity">Activity</option>
          <option value="Fragment">Fragment</option>
          <option value="Context">Context</option>
          <option value="View / binding">View / binding</option>
          <option value="ресурс">Ресурс</option>
          <option value="системный объект">Системный объект</option>
          <option value="пользовательский объект">Пользовательский объект</option>
        </select>
        <span class="registry-counter" data-code-registry-count></span>
      </div>
      <div class="problem-empty">По текущим фильтрам утечек не найдено.</div>
      <table class="code-problem-table leak-table">
        <thead>
          <tr>
            <th><button type="button" data-code-sort="severity">Риск</button></th>
            <th><button type="button" data-code-sort="class">Удержанный объект</button></th>
            <th><button type="button" data-code-sort="category">Тип</button></th>
            <th>Вероятный держатель</th>
            <th><button type="button" data-code-sort="score">Оценка</button></th>
            <th>Изменение</th>
            <th>Контекст</th>
            <th>{{tip "Оценка удержанного размера" "Ориентировочная оценка удержанного размера кандидата. Точное значение требует дампа памяти."}}</th>
            <th>{{tip "Мини-дерево доминирования" "Вероятная цепочка владения кандидата по контексту выполнения."}}</th>
            <th>Что проверить</th>
            <th>Доказательства</th>
          </tr>
        </thead>
        <tbody>
          {{range memoryLeakCompareRows .Comparison}}
          <tr data-code-problem-row data-score="{{printf "%.1f" .DeltaScore}}" data-severity="{{.Severity}}" data-class="{{.Candidate.ClassName}}" data-categories="{{.Candidate.ObjectKind}}" data-search="{{memoryLeakSearchText .Candidate}} {{.Status}}">
            <td><span class="problem-score {{severityClass .Severity}}">{{severityLabel .Severity}}</span><div class="muted">{{.Status}}</div></td>
            <td><div class="problem-location"><code>{{.Candidate.ClassName}}</code></div></td>
            <td><div class="leak-object-kind">{{.Candidate.ObjectKind}}</div>{{if .Candidate.SystemRetained}}<div class="muted">системный объект</div>{{else if .Candidate.UserOwned}}<div class="muted">пользовательский код</div>{{end}}</td>
            <td><code>{{.Candidate.Holder}}</code><div class="leak-holder-quality">{{.Candidate.HolderQuality}}</div></td>
            <td><div>база {{printf "%.1f" .BaselineScore}}</div><div>кандидат {{printf "%.1f" .Candidate.Score}}</div><div class="muted">дельта {{printf "%+.1f" .DeltaScore}}</div></td>
            <td><div>кол-во {{.BaselineCount}} → {{.Candidate.Count}} ({{printf "%+d" .DeltaCount}})</div><div>возраст {{humanDuration .BaselineAgeMS}} → {{humanDuration .Candidate.MaxAgeMS}} ({{signedDuration .DeltaAgeMS}})</div></td>
            <td><div class="problem-context">{{if .Candidate.Screen}}<div>экран <code>{{.Candidate.Screen}}</code></div>{{end}}{{if .Candidate.Flow}}<div>флоу <code>{{.Candidate.Flow}}</code></div>{{end}}{{if .Candidate.Step}}<div>шаг <code>{{.Candidate.Step}}</code></div>{{end}}</div></td>
            <td><div>{{dataSize .Candidate.EstimatedRetainedKB}}</div><div class="muted">{{.Candidate.RetainedSizeConfidence}}</div><div class="muted">{{.Candidate.RetainedSizeExplanation}}</div></td>
            <td><div class="leak-dominator">{{range .Candidate.DominatorPath}}<span>{{.}}</span>{{end}}</div><div class="muted">{{.Candidate.DominatorTreeExplanation}}</div><div class="leak-chain-summary">{{.Candidate.LeakChainSummary}}</div></td>
            <td>{{.Candidate.Recommendation}}<div class="leak-chain-actions"><strong>Быстрые проверки цепочки</strong>{{range .Candidate.LeakChainActions}}<span>{{.}}</span>{{end}}</div></td>
            <td>{{.Candidate.Evidence}}</td>
          </tr>
          {{else}}<tr><td colspan="11" class="muted">У кандидата нет подозрений на утечки памяти.</td></tr>{{end}}
        </tbody>
      </table>
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
        <tr><th>Метрика</th><th>База</th><th>Кандидат</th><th>Изменение</th><th>Регрессия</th><th>Серьезность</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th><th>Выборка</th><th>Интервал</th></tr>
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
      <tr><th>Метрика</th><th>Серьезность</th><th>Регрессия</th><th>{{tip "Доверие" (scoreHelp "confidence")}}</th><th>Выборка</th></tr>
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
      {{range significantReportFindings .Analysis.Findings}}
      <div class="finding {{severityClass .Severity}}"><strong>{{.Title}}</strong><div class="muted">{{.Detail}}</div></div>
      {{else}}<div class="muted">Нет значимых эвристических предупреждений.</div>{{end}}
    </div>
    <h3>Рекомендации</h3>
    <ul class="recommendations">
      {{range .Analysis.Recommendations}}<li>{{.}}</li>{{else}}<li>Нет дополнительных рекомендаций.</li>{{end}}
    </ul>
  </section>
</main>
<script>` + reportJS + `</script>
</body>
</html>`
