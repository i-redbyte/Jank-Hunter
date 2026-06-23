package io.jankhunter.plugin.execution

import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterInputPathsTest {
    @Test
    fun parsesCommaAndLineSeparatedPaths() {
        assertEquals(
            listOf("a.jhlog", "b.jhlog", "c/*.jhlog"),
            JankHunterInputPaths.pathList("a.jhlog, b.jhlog\nc/*.jhlog"),
        )
    }
}
