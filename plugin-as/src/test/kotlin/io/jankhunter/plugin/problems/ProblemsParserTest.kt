package io.jankhunter.plugin.problems

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class ProblemsParserTest {
    @Test
    fun parsesCsvWithQuotedValues() {
        val file = Files.createTempFile("jankhunter-problems", ".csv").toFile()
        file.writeText(
            """
            class,method,severity,score,categories,recommendation
            com.example.FeedRepository,load,high,42.0,"ui|network","Move work off main thread, then verify"
            """.trimIndent(),
        )

        val table = ProblemsParser.parse(file)

        assertEquals(listOf("class", "method", "severity", "score", "categories", "recommendation"), table.columns)
        assertEquals(1, table.rows.size)
        assertEquals("com.example.FeedRepository", table.rows[0]["class"])
        assertEquals("Move work off main thread, then verify", table.rows[0]["recommendation"])
    }

    @Test
    fun expandsJsonDrillDownRows() {
        val file = Files.createTempFile("jankhunter-problems", ".json").toFile()
        file.writeText(
            """
            [{
              "class_name": "com.example.FeedRepository",
              "severity": "critical",
              "categories": ["ui"],
              "drill_down": [
                {"method": "load", "screen": "Feed"},
                {"method": "refresh", "screen": "Feed"}
              ]
            }]
            """.trimIndent(),
        )

        val table = ProblemsParser.parse(file)

        assertEquals(2, table.rows.size)
        assertTrue("class_name" in table.columns)
        assertEquals("load", table.rows[0]["method"])
        assertEquals("refresh", table.rows[1]["method"])
    }
}
