package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationPackagesTest {
    @Test
    fun keepsManualIncludesWhenWholeApplicationIsDisabled() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = listOf("com.myapp.feature", " com.myapp.data. "),
            includeWholeApplication = false,
            androidNamespace = "com.myapp",
        )

        assertEquals(listOf("com.myapp.feature", "com.myapp.data"), includes)
    }

    @Test
    fun addsAndroidNamespaceWhenWholeApplicationIsEnabled() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = listOf("com.myapp.feature", "com.myapp"),
            includeWholeApplication = true,
            androidNamespace = " com.myapp. ",
        )

        assertEquals(listOf("com.myapp.feature", "com.myapp"), includes)
    }

    @Test
    fun ignoresBlankNamespace() {
        val includes = InstrumentationPackages.effectiveIncludes(
            manualIncludes = emptyList(),
            includeWholeApplication = true,
            androidNamespace = " ",
        )

        assertEquals(emptyList<String>(), includes)
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
