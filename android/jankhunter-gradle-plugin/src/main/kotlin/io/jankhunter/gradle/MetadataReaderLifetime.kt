package io.jankhunter.gradle

import org.gradle.api.services.BuildService
import org.gradle.api.services.BuildServiceParameters

/** Build-scoped ownership also closes reader JAR handles in a reused Gradle daemon. */
abstract class MetadataReaderLifetime : BuildService<BuildServiceParameters.None>, AutoCloseable {
    override fun close() {
        MetadataReaderMetrics.logLineOrNull()?.let { line -> println(line) }
        HookAbiValidationSession.clear()
        IsolatedMetadataReader.release()
        MetadataReaderMetrics.reset()
    }
}
