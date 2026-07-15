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

//go:embed templates/dependency-injection.tmpl
var dependencyInjectionTemplate string

//go:embed templates/leaks-inspect.tmpl
var leaksInspectTemplate string

//go:embed templates/leaks-compare.tmpl
var leaksCompareTemplate string

//go:embed templates/inspect.tmpl
var inspectTemplate string

//go:embed templates/compare.tmpl
var compareTemplate string

var (
	cachedInspectTemplate             = newCachedReportTemplate("inspect", inspectTemplate)
	cachedCompareTemplate             = newCachedReportTemplate("compare", compareTemplate)
	cachedMathInspectTemplate         = newCachedReportTemplate("math-inspect", mathInspectTemplate)
	cachedMathCompareTemplate         = newCachedReportTemplate("math-compare", mathCompareTemplate)
	cachedLeaksInspectTemplate        = newCachedReportTemplate("leaks-inspect", leaksInspectTemplate)
	cachedLeaksCompareTemplate        = newCachedReportTemplate("leaks-compare", leaksCompareTemplate)
	cachedInfluenceTemplate           = newCachedReportTemplate("influence", influenceTemplate)
	cachedDiagnosticsTemplate         = newCachedReportTemplate("diagnostics", diagnosticsTemplate)
	cachedDependencyInjectionTemplate = newCachedReportTemplate("dependency-injection", dependencyInjectionTemplate)
)
