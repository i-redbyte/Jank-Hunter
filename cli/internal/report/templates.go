package report

import _ "embed"

//go:embed templates/components.tmpl
var sharedComponentsTemplate string

//go:embed templates/base.css
var baseCSS string

//go:embed templates/modern.css
var modernCSS string

//go:embed templates/report.js
var reportJS string

//go:embed templates/problem-search.js
var problemSearchJS string

//go:embed templates/log-growth.css
var logGrowthCSS string

//go:embed templates/log-growth.js
var logGrowthJS string

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

//go:embed templates/thread-snapshot.js
var threadSnapshotJS string

//go:embed templates/thread-overview.js
var threadOverviewJS string

//go:embed templates/thread-insights.js
var threadInsightsJS string

//go:embed templates/concurrency-browser.js
var concurrencyBrowserJS string

//go:embed templates/thread-coroutines.js
var threadCoroutinesJS string

//go:embed templates/thread-resources.js
var threadResourcesJS string

//go:embed templates/worker-catalog.js
var workerCatalogJS string

//go:embed templates/work-registry.js
var workRegistryJS string

//go:embed templates/worker-attempts.js
var workerAttemptsJS string

//go:embed templates/worker-signals.js
var workerSignalsJS string

//go:embed templates/worker-compare.js
var workerCompareJS string

//go:embed templates/thread-lifecycle.js
var threadLifecycleJS string

//go:embed templates/work-submissions.js
var workSubmissionsJS string

//go:embed templates/work-chain.js
var workChainJS string

//go:embed templates/work-schedules.js
var workSchedulesJS string

//go:embed templates/work-schedules.css
var workSchedulesCSS string
