package io.jankhunter.runtime

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RuntimeSqlNormalizerTest {
    @Test
    fun removesBoundValuesAndNormalizesWhitespace() {
        assertEquals(
            "SELECT * FROM messages WHERE id = ? AND author = ?",
            RuntimeSqlNormalizer.normalize("  SELECT  * FROM messages WHERE id = 42 AND author = 'secret'  "),
        )
    }

    @Test
    fun rejectsNonSqlAndBoundsDynamicRoomStatements() {
        assertNull(RuntimeSqlNormalizer.normalize("not a query"))
        assertTrue(RuntimeSqlNormalizer.normalize("SELECT ${"column,".repeat(100)} value FROM table")!!.length <= 320)
    }

    @Test
    fun classifiesNormalizedOperationWithoutTrustingGeneratedMethodName() {
        assertEquals(4, RuntimeSqlNormalizer.operation("DELETE FROM messages WHERE id = ?", 1))
        assertEquals(2, RuntimeSqlNormalizer.operation("REPLACE INTO messages VALUES (?)", 1))
        assertEquals(3, RuntimeSqlNormalizer.operation(null, 3))
    }
}
