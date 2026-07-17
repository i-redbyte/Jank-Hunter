package io.jankhunter.gradle

import com.android.build.api.instrumentation.InstrumentationScope
import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterPluginTest {
    @Test
    fun applicationInstrumentationIncludesProjectDependencies() {
        assertEquals(
            InstrumentationScope.ALL,
            JankHunterPlugin().instrumentationScope(applicationProject = true),
        )
    }

    @Test
    fun libraryInstrumentationStaysInsideTheLibraryProject() {
        assertEquals(
            InstrumentationScope.PROJECT,
            JankHunterPlugin().instrumentationScope(applicationProject = false),
        )
    }
}
