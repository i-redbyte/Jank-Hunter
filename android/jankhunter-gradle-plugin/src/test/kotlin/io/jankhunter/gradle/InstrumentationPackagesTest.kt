package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class InstrumentationPackagesTest {
    @Test fun ownImplementationIsRejectedBeforeAnyFeatureOrGeneratedDiSelection() {
        val parameters = java.lang.reflect.Proxy.newProxyInstance(
            javaClass.classLoader, arrayOf(JankHunterInstrumentationParameters::class.java),
        ) { _, _, _ -> error("own implementations must not reach feature selection") } as JankHunterInstrumentationParameters
        val data = object : com.android.build.api.instrumentation.ClassData {
            override val className = "io.jankhunter.jankstats.DaggerFrameAdapter"
            override val classAnnotations = emptyList<String>()
            override val interfaces = emptyList<String>()
            override val superClasses = emptyList<String>()
        }
        assertFalse(InstrumentationClassSelector.evaluate(parameters, data, CausalCodeOrigin.PROJECT).any())
    }
    @Test fun jankStatsAdaptersAreExcludedInNormalAndWholeApplicationModes() {
        for (name in listOf("io.jankhunter.runtime.integration.AndroidXJankStatsAdapter", "io.jankhunter.jankstats.FrameAdapter")) {
            assertTrue(InstrumentationPackages.isBuiltinExcluded(name))
            assertTrue(isJankHunterSupportClass(name.replace('.', '/')))
            assertFalse(InstrumentationMatcher(listOf("io.jankhunter"), emptyList(), true).matches(name))
        }
        assertTrue(InstrumentationMatcher(emptyList(), emptyList(), true).matches("io.jankhunter.jankstatsextra.App"))
    }
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
        assertFalse(InstrumentationPackages.isBuiltinExcluded("kotlinxextra/coroutines/BuildersKt"))
        assertFalse(InstrumentationPackages.isBuiltinExcluded("io.jankhunter.okhttp3extra.ApplicationClient"))
        assertTrue(InstrumentationPackages.isGeneratedAndroidClass("com/example/R\$string"))
        assertFalse(InstrumentationPackages.isBuiltinExcluded("com/example/FeedRepository"))
        assertFalse(InstrumentationPackages.isGeneratedAndroidClass("com/example/FeedRepository"))
    }
}
