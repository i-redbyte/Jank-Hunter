package io.jankhunter.gradle

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Test

class ArtTiInstrumentationFootprintTest {
    @Test
    fun aggregatesDiagnosticsJsonl() {
        val file = Files.createTempFile("artti-footprint", ".jsonl").toFile()
        file.writeText(
            """
            {"format":2,"pass":"main","class":"app.A","methods":10,"ignoredMethods":0,"annotatedMethods":1,"methodFilterIncluded":10,"methodFilterExcluded":0,"methodFilterReasons":[],"skippedMethods":[],"hooks":[{"intent":"x","signature":"x","count":3,"method":"run()V"}],"decisions":[],"annotations":[]}
            {"format":2,"pass":"lifecycle","class":"app.B","methods":4,"ignoredMethods":0,"annotatedMethods":0,"methodFilterIncluded":4,"methodFilterExcluded":0,"methodFilterReasons":[],"skippedMethods":[],"hooks":[{"intent":"y","signature":"y","count":1,"method":"load()V"}],"decisions":[],"annotations":[]}
            """.trimIndent(),
        )
        val footprint = ArtTiInstrumentationFootprintReader.read(file, gradleModuleCount = 3)
        requireNotNull(footprint)
        assertEquals(2, footprint.instrumentedClassCount)
        assertEquals(14, footprint.instrumentedMethodCount)
        assertEquals(4, footprint.hookCount)
        assertEquals(2, footprint.instrumentationPassCount)
        assertEquals(3, footprint.gradleModuleCount)
    }
}
