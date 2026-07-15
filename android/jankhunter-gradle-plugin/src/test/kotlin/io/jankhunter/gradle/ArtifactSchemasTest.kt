package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtifactSchemasTest {
    @Test
    fun artifactPathIsScopedByEveryPersistedSchema() {
        val path = ArtifactSchemas.instrumentationArtifactsPath("vkteamsDevDebug")

        assertTrue(path.startsWith("intermediates/jankhunter/vkteamsDevDebug/instrumentation-artifacts/"))
        assertTrue(path.endsWith(ArtifactSchemas.instrumentationLayoutFingerprint))
        assertTrue(path.contains("owner-map-v${ArtifactSchemas.OWNER_MAP_FORMAT}"))
        assertTrue(path.contains("class-graph-v${ArtifactSchemas.CLASS_GRAPH_FORMAT}"))
        assertTrue(path.contains("diagnostics-v${ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT}"))
        assertTrue(path.contains("dependency-injection-v${ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT}"))
    }

    @Test
    fun everySchemaChangeInvalidatesTheInstrumentationLayout() {
        val current = fingerprint()

        assertNotEquals(current, fingerprint(ownerMap = ArtifactSchemas.OWNER_MAP_FORMAT + 1))
        assertNotEquals(current, fingerprint(classGraph = ArtifactSchemas.CLASS_GRAPH_FORMAT + 1))
        assertNotEquals(
            current,
            fingerprint(diagnostics = ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT + 1),
        )
        assertNotEquals(
            current,
            fingerprint(dependencyInjection = ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT + 1),
        )
        assertEquals(ArtifactSchemas.instrumentationLayoutFingerprint, current)
    }

    private fun fingerprint(
        ownerMap: Int = ArtifactSchemas.OWNER_MAP_FORMAT,
        classGraph: Int = ArtifactSchemas.CLASS_GRAPH_FORMAT,
        diagnostics: Int = ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT,
        dependencyInjection: Int = ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT,
    ): String {
        return ArtifactSchemas.instrumentationLayoutFingerprint(
            ownerMapFormat = ownerMap,
            classGraphFormat = classGraph,
            instrumentationDiagnosticsFormat = diagnostics,
            dependencyInjectionCatalogFormat = dependencyInjection,
        )
    }
}
