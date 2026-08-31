package io.jankhunter.gradle

internal object ArtifactSchemas {
    const val ARTIFACT_METADATA_FORMAT = 1
    const val CLASS_GRAPH_FORMAT = 1
    const val INSTRUMENTATION_DIAGNOSTICS_FORMAT = 1
    const val DEPENDENCY_INJECTION_CATALOG_FORMAT = 1
    const val ANDROID_COMPONENT_CATALOG_FORMAT = 1

    val instrumentationLayoutFingerprint: String
        get() = instrumentationLayoutFingerprint(
            classGraphFormat = CLASS_GRAPH_FORMAT,
            instrumentationDiagnosticsFormat = INSTRUMENTATION_DIAGNOSTICS_FORMAT,
            dependencyInjectionCatalogFormat = DEPENDENCY_INJECTION_CATALOG_FORMAT,
            androidComponentCatalogFormat = ANDROID_COMPONENT_CATALOG_FORMAT,
        )

    fun instrumentationArtifactsPath(variantName: String): String {
        return "intermediates/jankhunter/$variantName/instrumentation-artifacts/" +
            instrumentationLayoutFingerprint
    }

    internal fun instrumentationLayoutFingerprint(
        classGraphFormat: Int,
        instrumentationDiagnosticsFormat: Int,
        dependencyInjectionCatalogFormat: Int,
        androidComponentCatalogFormat: Int,
    ): String {
        return "class-graph-v${classGraphFormat}_" +
            "diagnostics-v${instrumentationDiagnosticsFormat}_" +
            "dependency-injection-v${dependencyInjectionCatalogFormat}_" +
            "android-components-v$androidComponentCatalogFormat"
    }
}
