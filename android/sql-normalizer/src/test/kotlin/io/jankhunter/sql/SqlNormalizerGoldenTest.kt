package io.jankhunter.sql

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SqlNormalizerGoldenTest {
    @Test
    fun removesCommentsAndLiteralValuesButPreservesQuotedIdentifiers() {
        assertEquals(
            "SELECT \"user name\", `order`, [group] FROM accounts WHERE email=? AND id=?",
            SqlNormalizer.normalize(
                "-- tenant=42 alice@example.com\n" +
                    "/* private account */ SELECT \"user name\", `order`, [group] " +
                    "FROM accounts WHERE email='alice@example.com' AND id=0x2A",
            ),
        )
    }

    @Test
    fun canonicalizesAllSQLiteBindPlaceholderForms() {
        assertEquals(
            "SELECT * FROM messages WHERE a=? AND b=? AND c=? AND d=?",
            SqlNormalizer.normalize(
                "SELECT * FROM messages WHERE a=:account_id AND b=@email AND c=\$tenant AND d=?42",
            ),
        )
    }

    @Test
    fun collapsesLiteralListsIndependentOfTheirSize() {
        val shortIn = SqlNormalizer.normalize("SELECT * FROM messages WHERE id IN (1, 2)")
        val longIn = SqlNormalizer.normalize(
            "SELECT * FROM messages WHERE id IN (${(1..1_000).joinToString()})",
        )
        assertEquals("SELECT * FROM messages WHERE id IN (?)", shortIn)
        assertEquals(shortIn, longIn)

        val shortValues = SqlNormalizer.normalize("INSERT INTO messages(id, body) VALUES (1, 'one')")
        val longValues = SqlNormalizer.normalize(
            "INSERT INTO messages(id, body) VALUES " +
                (1..1_000).joinToString { "($it, 'private-$it')" },
        )
        assertEquals("INSERT INTO messages(id, body) VALUES (?)", shortValues)
        assertEquals(shortValues, longValues)
    }

    @Test
    fun keepsSubqueryShapeInsteadOfTreatingItAsLiteralList() {
        assertEquals(
            "SELECT * FROM messages WHERE id IN (SELECT message_id FROM labels WHERE name=?)",
            SqlNormalizer.normalize(
                "SELECT * FROM messages WHERE id IN (SELECT message_id FROM labels WHERE name='private')",
            ),
        )
    }

    @Test
    fun handlesLeadingCommentsPragmaDdlUnicodeAndMalformedInput() {
        assertEquals("PRAGMA table_info(\"сообщения\")", SqlNormalizer.normalize("/* secret */ PRAGMA table_info(\"сообщения\")"))
        assertEquals("CREATE TABLE \"сообщения\" (id INTEGER, body TEXT)", SqlNormalizer.normalize("CREATE TABLE \"сообщения\" (id INTEGER, body TEXT)"))
        assertEquals("SELECT * FROM messages WHERE body=?", SqlNormalizer.normalize("SELECT * FROM messages WHERE body='секрет"))
        assertNull(SqlNormalizer.normalize("/* SELECT private */ open chat screen"))
    }

    @Test
    fun neverEmitsSensitivePayloadFromLiteralOrCommentStates() {
        val secrets = listOf("mail@example.com", "auth-token-123", "личный-текст")
        val sql = "/* ${secrets[0]} */ SELECT * FROM data WHERE a='${secrets[1]}' " +
            "AND b=\"safe identifier\" -- ${secrets[2]}"
        val normalized = requireNotNull(SqlNormalizer.normalize(sql))
        for (secret in secrets) assertFalse("leaked $secret in $normalized", normalized.contains(secret))
        assertTrue(normalized.contains("\"safe identifier\""))
    }

    @Test
    fun outputAndInputWorkAreBoundedAndDeterministic() {
        val prefix = "SELECT ${"column,".repeat(100)} value FROM messages WHERE id IN ("
        val first = requireNotNull(SqlNormalizer.normalize(prefix + "1,".repeat(100_000) + "2)"))
        val second = requireNotNull(SqlNormalizer.normalize(prefix + "9,".repeat(100_000) + "8)"))
        assertTrue(first.length <= SqlNormalizer.MAX_TEMPLATE_LENGTH)
        assertEquals(first, second)
    }

    @Test
    fun fingerprintIsStableFnv1aOverUtf8WithoutAllocationHelpers() {
        assertEquals(0xa430d84680aabd0bUL.toLong(), SqlNormalizer.fingerprint("hello"))
        assertEquals(0L, SqlNormalizer.fingerprint(null))
        val template = requireNotNull(SqlNormalizer.normalize("SELECT \"сообщения\" WHERE id=42"))
        assertEquals(SqlNormalizer.fingerprint(template), SqlNormalizer.fingerprint(template))
    }

    @Test
    fun classifiesOuterStatementAfterCommonTableExpressions() {
        assertEquals(
            2,
            SqlNormalizer.operation(
                "WITH RECURSIVE seed(id) AS (SELECT ? UNION ALL SELECT id + ? FROM seed WHERE id < ?) " +
                    "INSERT INTO messages(id) SELECT id FROM seed",
                1,
            ),
        )
        assertEquals(
            3,
            SqlNormalizer.operation(
                "WITH first(value) AS (SELECT (?)), second AS (SELECT value FROM first) " +
                    "UPDATE messages SET body = ? WHERE id IN (SELECT value FROM second)",
                1,
            ),
        )
        assertEquals(
            4,
            SqlNormalizer.operation(
                "WITH \"cte(with-parenthesis)\" AS (SELECT ?) DELETE FROM messages WHERE id = ?",
                1,
            ),
        )
        assertEquals(1, SqlNormalizer.operation("WITH sample AS (SELECT ?) SELECT * FROM sample", 5))
        assertEquals(5, SqlNormalizer.operation("WITH sample AS (SELECT ?)", 5))
    }
}
