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

        assertEquals(
            linkedSetOf("com.myapp", "com.myapp.feature", "com.myapp.data"),
            instrumentation.includePackages,
        )
        assertEquals(
            linkedSetOf("com.myapp.generated", "com.myapp.di", "com.myapp.legacy"),
            instrumentation.excludePackages,
        )
    }
}
