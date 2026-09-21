package io.jankhunter.gradle

import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class LifecycleMetadataStoreTest {
    @get:Rule val temporary = TemporaryFolder()

    @Test
    fun heapPressureChangesTheCacheWithoutDroppingHeaders() {
        var available = 512L * 1024 * 1024
        val header = LifecycleClassHeader("app/Child", "app/Base", listOf("app/Interface"), Opcodes.ACC_PUBLIC,
            mapOf("onDestroy" to Opcodes.ACC_FINAL), true, setOf("Lapp/Marker;"), mapOf("visit()V" to 1), true)
        LifecycleMetadataStore(temporary.newFile("adaptive.mv")) { available }.use { store ->
            store.put(header)
            val initialCache = store.cacheMiB
            assertEquals(16, initialCache)
            available /= 8
            store.maintainBudget()
            assertEquals(2, store.cacheMiB)
            assertEquals(header, store.header(header.name))
            available *= 8
            store.maintainBudget()
            assertEquals(initialCache, store.cacheMiB)
            assertEquals(listOf(header), store.headers().toList())
        }
    }

    @Test
    fun cacheUnitsRemainCorrectAtLowMemoryAndTheSetterIntegerBoundary() {
        var available = 0L
        LifecycleMetadataStore(temporary.newFile("cache-units.mv")) { available }.use { store ->
            for ((heap, expectedMiB) in listOf(-1L to 1, 0L to 1, 1024L to 1,
                (512L * 1024 * 1024) to 16, Long.MAX_VALUE to (Int.MAX_VALUE / 1024))) {
                available = heap
                store.maintainBudget()
                assertEquals("available heap bytes=$heap", expectedMiB, store.cacheMiB)
            }
        }
    }

    @Test
    fun archiveClassesAreSpilledAndReadInCanonicalOrder() {
        val archive = temporary.newFile("input.jar")
        val first = type("app/A")
        val last = type("app/Z")
        ZipOutputStream(archive.outputStream()).use { zip ->
            listOf("app/Z.class" to last, "META-INF/versions/9/app/A.class" to last,
                "module-info.class" to first, "app/A.class" to first).forEach { (name, bytes) ->
                zip.putNextEntry(ZipEntry(name))
                zip.write(bytes)
                zip.closeEntry()
            }
        }
        LifecycleClassIndex(temporary.newFile("headers.mv")).use { index ->
            LifecycleClassSpool(temporary.newFile("classes.bin"), index).use { spool ->
                LifecycleClassInputs.forEach(archive, spool::add)
                assertEquals(listOf("app/A", "app/Z"), index.programClasses().map { it.name }.toList())
                assertArrayEquals(first, spool.read("app/A"))
                assertArrayEquals(last, spool.read("app/Z"))
            }
        }
    }

    @Test
    fun diskIndexPreservesReplacementAndDuplicateChecks() {
        LifecycleClassIndex(temporary.newFile("duplicates.mv")).use { index ->
            index.add(type("app/A"), false)
            index.add(type("app/A"), true)
            index.add(type("app/A"), false)
            assertTrue(index.header("app/A")!!.programClass)
            org.junit.Assert.assertThrows(IllegalArgumentException::class.java) { index.add(type("app/A"), true) }
        }
    }

    private fun type(name: String): ByteArray = ClassWriter(0).apply {
        visit(Opcodes.V17, Opcodes.ACC_PUBLIC, name, null, "java/lang/Object", null)
        visitEnd()
    }.toByteArray()
}
