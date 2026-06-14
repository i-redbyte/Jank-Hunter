package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Test

class JankHunterExtensionTest {
    @Test
    fun instrumentationDslAcceptsPackageLists() {
        val instrumentation = JankHunterExtension.Instrumentation()

        instrumentation.includePackages("com.myapp", " com.myapp.feature ")
        instrumentation.includePackages(listOf("com.myapp.data", ""))
        instrumentation.excludePackages("com.myapp.generated", "com.myapp.di")
        instrumentation.excludePackages(listOf("com.myapp.legacy"))
        instrumentation.includeWholeApplication = true
        instrumentation.asmProgressLog = true
        instrumentation.classGraph = true
        instrumentation.runtimeCallGraph = true

        assertEquals(
            linkedSetOf("com.myapp", "com.myapp.feature", "com.myapp.data"),
            instrumentation.includePackages,
        )
        assertEquals(
            linkedSetOf("com.myapp.generated", "com.myapp.di", "com.myapp.legacy"),
            instrumentation.excludePackages,
        )
        assertEquals(true, instrumentation.includeWholeApplication)
        assertEquals(true, instrumentation.asmProgressLog)
        assertEquals(true, instrumentation.classGraph)
        assertEquals(true, instrumentation.runtimeCallGraph)
    }

    @Test
    fun wholeApplicationIncludeIsOptIn() {
        val instrumentation = JankHunterExtension.Instrumentation()

        assertEquals(false, instrumentation.includeWholeApplication)
        assertEquals(false, instrumentation.asmProgressLog)
        assertEquals(true, instrumentation.classGraph)
        assertEquals(false, instrumentation.runtimeCallGraph)
    }
}
