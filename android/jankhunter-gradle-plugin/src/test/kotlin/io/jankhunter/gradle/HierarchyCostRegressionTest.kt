package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class HierarchyCostRegressionTest {
    @Test
    fun completeAgpHierarchyIsLoadedOnceAndThenCached() {
        val parents = List(40) { "example.Parent$it" }
        val interfaces = List(40) { "example.Contract$it" }
        val root = Data("example.Root", parents, interfaces)
        var loads = 0
        val resolver = ClassHierarchyResolver(object : ClassContext {
            override val currentClassData: ClassData = root
            override fun loadClassData(className: String): ClassData? {
                loads++
                return root.takeIf { className == root.className }
            }
        })
        val expected = (listOf(root.className) + parents + interfaces).map { it.replace('.', '/') }.toSet()
        assertEquals(expected, resolver.resolve(root.className))
        assertEquals("AGP already returns all ancestors and interfaces", 1, loads)
        assertEquals(expected, resolver.resolve(root.className))
        assertEquals(1, loads)
        assertTrue(resolver.diagnostics().isEmpty())
    }

    @Test
    fun generatedDiNamesDoNotForceLazyAgpHierarchy() {
        val cases = mapOf(
            "example.DaggerRoot" to DependencyInjectionFramework.DAGGER2,
            "example.Service_Factory" to DependencyInjectionFramework.DAGGER2,
            "example.Hilt_Activity" to DependencyInjectionFramework.HILT,
            "org.koin.ksp.generated.DefaultModule" to DependencyInjectionFramework.KOIN,
        )
        for ((name, framework) in cases) {
            val data = object : ClassData {
                override val className = name
                override val classAnnotations = emptyList<String>()
                override val interfaces: List<String> get() = error("unnecessary interface traversal")
                override val superClasses: List<String> get() = error("unnecessary ancestor traversal")
            }
            assertTrue(DependencyInjectionClassMatcher.isGeneratedDiClass(data))
            assertEquals(framework, DependencyInjectionClassMatcher.generatedFramework(data))
        }
    }

    private data class Data(
        override val className: String,
        override val superClasses: List<String>,
        override val interfaces: List<String>,
        override val classAnnotations: List<String> = emptyList(),
    ) : ClassData
}
