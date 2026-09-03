package io.jankhunter.gradle

import com.android.build.api.instrumentation.ClassContext
import com.android.build.api.instrumentation.ClassData
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class ClassHierarchyResolverTest {
    @Test
    fun missingClasspathClassIsExpectedAndDoesNotCreateDiagnostic() {
        val context = FakeClassContext { className ->
            when (className) {
                "example.Root" -> FakeClassData(className, superClasses = listOf("missing.Parent"))
                else -> null
            }
        }
        val resolver = ClassHierarchyResolver(context)

        assertEquals(setOf("example/Root", "missing/Parent"), resolver.resolve("example/Root"))
        assertTrue(resolver.diagnostics().isEmpty())
    }

    @Test
    fun unexpectedMetadataFailureIsDiagnosedAndRemainingHierarchyIsResolved() {
        val context = FakeClassContext { className ->
            when (className) {
                "example.Root" -> FakeClassData(
                    className,
                    superClasses = listOf("broken.Parent", "example.GoodParent"),
                )
                "broken.Parent" -> throw IllegalStateException("invalid class metadata")
                "example.GoodParent" -> FakeClassData(className, interfaces = listOf("example.Contract"))
                else -> null
            }
        }
        val resolver = ClassHierarchyResolver(context)

        assertEquals(
            setOf("example/Root", "broken/Parent", "example/GoodParent", "example/Contract"),
            resolver.resolve("example/Root"),
        )
        assertEquals(
            mapOf(
                ClassHierarchyResolutionFailure(
                    className = "broken.Parent",
                    errorType = "java.lang.IllegalStateException",
                    detail = "invalid class metadata",
                ) to 1,
            ),
            resolver.diagnostics(),
        )
    }

    @Test
    fun virtualMachineErrorsAreNotSuppressed() {
        val failure = AssertionError("corrupt runtime")
        val resolver = ClassHierarchyResolver(FakeClassContext { throw failure })

        try {
            resolver.resolve("example.Root")
            fail("ClassHierarchyResolver suppressed an Error")
        } catch (actual: AssertionError) {
            assertTrue(actual === failure)
        }
    }

    private class FakeClassContext(
        private val loader: (String) -> ClassData?,
    ) : ClassContext {
        override val currentClassData: ClassData = FakeClassData("example.Root")

        override fun loadClassData(className: String): ClassData? = loader(className)
    }

    private data class FakeClassData(
        override val className: String,
        override val classAnnotations: List<String> = emptyList(),
        override val interfaces: List<String> = emptyList(),
        override val superClasses: List<String> = emptyList(),
    ) : ClassData
}
