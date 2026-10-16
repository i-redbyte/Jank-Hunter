package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Opcodes

class WorkerCatalogTest {
    @Test
    fun includesConcreteInheritedWorkersWithoutLoadingOrInstantiatingThem() {
        LifecycleClassIndex().use { index ->
            index.add(type("library/Base", "androidx/work/CoroutineWorker", abstract = true), true)
            index.add(type("app/NeverRun", "library/Base"), true)
            index.add(type("app/Plain", "java/lang/Object"), true)
            val catalog = workerCatalog(index, 10)
            assertEquals(listOf("app.NeverRun"), catalog.classes)
            assertEquals(0, catalog.unresolvedClasses)
            assertEquals(0, catalog.omittedWorkers)
        }
    }

    @Test
    fun missingHierarchyAndLimitAreExplicitInsteadOfClaimingCompleteInventory() {
        LifecycleClassIndex().use { index ->
            index.add(type("app/A", "androidx/work/Worker"), true)
            index.add(type("app/B", "androidx/work/ListenableWorker"), true)
            index.add(type("app/Unknown", "missing/Base"), true)
            val catalog = workerCatalog(index, 1)
            assertEquals(1, catalog.classes.size)
            assertEquals(1, catalog.unresolvedClasses)
            assertEquals(1, catalog.omittedWorkers)
        }
    }

    @Test
    fun classpathOnlyWorkersAreOutsideProgramScopeAndCyclesAreUnresolved() {
        LifecycleClassIndex().use { index ->
            index.add(type("library/NotPackaged", "androidx/work/Worker"), false)
            index.add(type("app/A", "app/B"), true)
            index.add(type("app/B", "app/A"), true)
            val catalog = workerCatalog(index, 10)
            assertTrue(catalog.classes.isEmpty())
            assertEquals(2, catalog.unresolvedClasses)
        }
    }

    @Test
    fun inventoryAlsoBoundsEncodedBytesWithAnExplicitOmissionCount() {
        LifecycleClassIndex().use { index ->
            repeat(300) { number -> index.add(type("app/" + "A".repeat(1000) + number, "androidx/work/Worker"), true) }
            val catalog = workerCatalog(index, 4096)
            assertTrue(catalog.assetText("0".repeat(32)).toByteArray(Charsets.UTF_8).size <= 128 * 1024)
            assertEquals(300, catalog.classes.size + catalog.omittedWorkers)
            assertTrue(catalog.omittedWorkers > 0)
        }
    }

    private fun type(name: String, parent: String, abstract: Boolean = false): ByteArray = ClassWriter(0).apply {
        visit(Opcodes.V1_8, Opcodes.ACC_PUBLIC or if (abstract) Opcodes.ACC_ABSTRACT else 0, name, null, parent, null)
        visitEnd()
    }.toByteArray()
}
