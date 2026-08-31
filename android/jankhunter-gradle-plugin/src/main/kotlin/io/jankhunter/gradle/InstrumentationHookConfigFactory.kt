package io.jankhunter.gradle

/** Single mapping boundary from Gradle providers to the immutable ASM instrumentation model. */
internal object InstrumentationHookConfigFactory {
    fun create(params: JankHunterInstrumentationParameters): HookConfig = HookConfig(
        autoInit = params.autoInit.getOrElse(false),
        methodCounters = params.methodCounters.getOrElse(false),
        methodFilterMode = params.methodFilterMode.getOrElse(JankHunterMethodFilterMode.FILTER),
        okhttp = params.okhttp.getOrElse(false),
        webSockets = params.webSockets.getOrElse(false),
        okHttpHelperAvailable = params.okHttpHelperAvailable.getOrElse(false),
        handlers = params.handlers.getOrElse(false),
        executors = params.executors.getOrElse(false),
        coroutines = params.coroutines.getOrElse(false),
        interactionOperations = params.interactionOperations.getOrElse(false),
        logSpam = params.logSpam.getOrElse(false),
        classGraph = params.classGraph.getOrElse(false),
        runtimeCallGraph = params.runtimeCallGraph.getOrElse(false),
        composeTracing = params.composeTracing.getOrElse(true),
        roomTracing = params.roomTracing.getOrElse(true),
        databaseTracing = params.databaseTracing.getOrElse(true),
        workerTracing = params.workerTracing.getOrElse(true),
        androidComponents = params.androidComponents.getOrElse(false),
        binderIPC = params.binderIPC.getOrElse(false),
        ioTracing = params.ioTracing.getOrElse(false),
        classGraphDirectory = params.classGraphDirectory.getOrElse(""),
        instrumentationDiagnosticsDirectory = params.instrumentationDiagnosticsDirectory.getOrElse(""),
        androidComponentCatalogDirectory = params.androidComponentCatalogDirectory.getOrElse(""),
        lifecycleLeaks = params.lifecycleLeaks.getOrElse(false),
    )
}
