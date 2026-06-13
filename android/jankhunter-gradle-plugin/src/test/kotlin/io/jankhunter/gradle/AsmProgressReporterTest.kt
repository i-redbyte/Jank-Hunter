package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AsmProgressReporterTest {
    @Test
    fun progressLineIsSingleLineAndRussian() {
        val line = formatAsmProgressLine(
            AsmProgressSnapshot(
                label = ":app:debug",
                scanned = 120,
                matched = 40,
                instrumented = 25,
                queued = 15,
                ratePerSecond = 12.5,
                etaSeconds = 1.2,
                latestClass = "com/myapp/feature/feed/FeedRepository",
                hooks = "methods+handler+coroutine",
            ),
        )

        assertEquals(false, line.contains('\n'))
        assertEquals(false, line.contains('\r'))
        assertTrue(line.contains("готово=25/40"))
        assertTrue(line.contains("очередь=15"))
        assertTrue(line.contains("ETA~2с"))
        assertTrue(line.contains("класс=com.myapp.feature.feed.FeedRepository"))
    }
}
