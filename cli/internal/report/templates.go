package report

import _ "embed"

//go:embed templates/base.css
var baseCSS string

//go:embed templates/report.js
var reportJS string

//go:embed templates/math.css
var mathCSS string

//go:embed templates/math-inspect.tmpl
var mathInspectTemplate string

//go:embed templates/math-compare.tmpl
var mathCompareTemplate string

//go:embed templates/influence.tmpl
var influenceTemplate string

//go:embed templates/diagnostics.tmpl
var diagnosticsTemplate string

//go:embed templates/inspect.tmpl
var inspectTemplate string

//go:embed templates/compare.tmpl
var compareTemplate string
