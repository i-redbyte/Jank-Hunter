package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ArtifactSchemasTest {
    @Test
    fun artifactPathIsScopedByEveryPersistedSchema() {
        val path = ArtifactSchemas.instrumentationArtifactsPath("vkteamsDevDebug")

        assertTrue(path.startsWith("intermediates/jankhunter/vkteamsDevDebug/instrumentation-artifacts/"))
        assertTrue(path.endsWith(ArtifactSchemas.instrumentationLayoutFingerprint))
        assertFalse(path.contains("owner-map"))
        assertTrue(path.contains("class-graph-v${ArtifactSchemas.CLASS_GRAPH_FORMAT}"))
        assertTrue(path.contains("diagnostics-v${ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT}"))
        assertTrue(path.contains("dependency-injection-v${ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT}"))
        assertTrue(path.contains("android-components-v${ArtifactSchemas.ANDROID_COMPONENT_CATALOG_FORMAT}"))
    }

    @Test
    fun everySchemaChangeInvalidatesTheInstrumentationLayout() {
        val current = fingerprint()

        assertNotEquals(current, fingerprint(classGraph = ArtifactSchemas.CLASS_GRAPH_FORMAT + 1))
        assertNotEquals(
            current,
            fingerprint(diagnostics = ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT + 1),
        )
        assertNotEquals(
            current,
            fingerprint(dependencyInjection = ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT + 1),
        )
        assertNotEquals(
            current,
            fingerprint(androidComponents = ArtifactSchemas.ANDROID_COMPONENT_CATALOG_FORMAT + 1),
        )
        assertEquals(ArtifactSchemas.instrumentationLayoutFingerprint, current)
    }

    private fun fingerprint(
        classGraph: Int = ArtifactSchemas.CLASS_GRAPH_FORMAT,
        diagnostics: Int = ArtifactSchemas.INSTRUMENTATION_DIAGNOSTICS_FORMAT,
        dependencyInjection: Int = ArtifactSchemas.DEPENDENCY_INJECTION_CATALOG_FORMAT,
        androidComponents: Int = ArtifactSchemas.ANDROID_COMPONENT_CATALOG_FORMAT,
    ): String {
        return ArtifactSchemas.instrumentationLayoutFingerprint(
            classGraphFormat = classGraph,
            instrumentationDiagnosticsFormat = diagnostics,
            dependencyInjectionCatalogFormat = dependencyInjection,
            androidComponentCatalogFormat = androidComponents,
        )
    }
}
