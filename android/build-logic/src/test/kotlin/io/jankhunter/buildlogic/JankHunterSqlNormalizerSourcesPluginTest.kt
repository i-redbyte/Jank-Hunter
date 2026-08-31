package io.jankhunter.buildlogic

import java.nio.file.Files
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test

class JankHunterSqlNormalizerSourcesPluginTest {
    @Test
    fun locatesCanonicalComponentFromNestedProject() {
        val root = Files.createTempDirectory("jankhunter-sql-normalizer-test").toFile()
        try {
            val main = root.resolve("sql-normalizer/src/main/kotlin").apply { mkdirs() }
            val test = root.resolve("sql-normalizer/src/test/kotlin").apply { mkdirs() }
            val nested = root.resolve("nested/plugin").apply { mkdirs() }

            val sources = locateSqlNormalizerSources(nested)

            assertEquals(main.canonicalFile, sources.main)
            assertEquals(test.canonicalFile, sources.test)
        } finally {
            root.deleteRecursively()
        }
    }

    @Test
    fun rejectsComponentWithoutTestContract() {
        val root = Files.createTempDirectory("jankhunter-sql-normalizer-invalid-test").toFile()
        try {
            root.resolve("sql-normalizer/src/main/kotlin").mkdirs()
            assertThrows(IllegalStateException::class.java) {
                locateSqlNormalizerSources(root)
            }
        } finally {
            root.deleteRecursively()
        }
    }
}
