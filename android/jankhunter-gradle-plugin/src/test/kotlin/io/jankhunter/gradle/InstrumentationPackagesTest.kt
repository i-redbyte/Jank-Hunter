package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationPackagesTest {
    @Test
    fun alwaysKeepsNamespaceAsSafeProjectBoundary() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = listOf("com.myapp.feature", " com.myapp.data. "),
            androidNamespace = "com.myapp",
        )

        assertEquals(linkedSetOf("com.myapp", "com.myapp.feature", "com.myapp.data"), includes)
    }

    @Test
    fun deduplicatesNormalizedAndroidNamespace() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = listOf("com.myapp.feature", "com.myapp"),
            androidNamespace = " com.myapp. ",
        )

        assertEquals(linkedSetOf("com.myapp", "com.myapp.feature"), includes)
    }

    @Test
    fun ignoresBlankNamespace() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = emptyList(),
            androidNamespace = " ",
        )

        assertEquals(emptySet<String>(), includes)
    }

    @Test
    fun sharedPackageHelpersClassifyGeneratedAndBuiltinClasses() {
        assertTrue(InstrumentationPackages.isBuiltinExcluded("kotlinx/coroutines/BuildersKt"))
        assertTrue(InstrumentationPackages.isBuiltinExcluded("org.jetbrains.annotations.NotNull"))
        assertTrue(InstrumentationPackages.isGeneratedAndroidClass("com/example/R\$string"))
        assertFalse(InstrumentationPackages.isBuiltinExcluded("com/example/FeedRepository"))
        assertFalse(InstrumentationPackages.isGeneratedAndroidClass("com/example/FeedRepository"))
    }
}
