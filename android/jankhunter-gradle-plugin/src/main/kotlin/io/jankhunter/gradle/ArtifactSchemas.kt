package io.jankhunter.gradle

internal object ArtifactSchemas {
    const val OWNER_MAP_FORMAT = 4
    const val CLASS_GRAPH_FORMAT = 1
    const val INSTRUMENTATION_DIAGNOSTICS_FORMAT = 1
    const val DEPENDENCY_INJECTION_CATALOG_FORMAT = 1

    val instrumentationLayoutFingerprint: String
        get() = instrumentationLayoutFingerprint(
            ownerMapFormat = OWNER_MAP_FORMAT,
            classGraphFormat = CLASS_GRAPH_FORMAT,
            instrumentationDiagnosticsFormat = INSTRUMENTATION_DIAGNOSTICS_FORMAT,
            dependencyInjectionCatalogFormat = DEPENDENCY_INJECTION_CATALOG_FORMAT,
        )

    fun instrumentationArtifactsPath(variantName: String): String {
        return "intermediates/jankhunter/$variantName/instrumentation-artifacts/" +
            instrumentationLayoutFingerprint
    }

    internal fun instrumentationLayoutFingerprint(
        ownerMapFormat: Int,
        classGraphFormat: Int,
        instrumentationDiagnosticsFormat: Int,
        dependencyInjectionCatalogFormat: Int,
    ): String {
        return "owner-map-v${ownerMapFormat}_" +
            "class-graph-v${classGraphFormat}_" +
            "diagnostics-v${instrumentationDiagnosticsFormat}_" +
            "dependency-injection-v$dependencyInjectionCatalogFormat"
    }
}
