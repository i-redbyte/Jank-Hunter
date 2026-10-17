package io.jankhunter.gradle

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HookAbiRequirementsTest {
    private fun instrumentation(
        okhttp: Boolean = false,
        lifecycleLeaks: Boolean = false,
        workerTracing: Boolean = false,
    ) = JankHunterResolvedInstrumentation(
        dependencyInjectionAnalysis = false,
        methodCounters = false,
        methodFilterMode = JankHunterMethodFilterMode.NONE,
        okhttp = okhttp,
        webSockets = false,
        handlers = false,
        executors = false,
        threadLifecycle = false,
        concurrencyDiagnostics = false,
        coroutines = false,
        interactionOperations = false,
        lifecycleLeaks = lifecycleLeaks,
        logSpam = false,
        classGraph = false,
        runtimeCallGraph = false,
        composeTracing = false,
        roomTracing = false,
        databaseTracing = false,
        workerTracing = workerTracing,
        androidComponents = false,
        binderIPC = false,
        ioTracing = false,
        scope = JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES,
    )

    @Test fun okhttpDisabledDoesNotRequireTransportOwners() {
        val owners = HookAbiRequirements.requiredOwners(instrumentation(okhttp = false), autoInit = true)
        assertFalse(owners.any { it.contains("okhttp3") })
        assertTrue("io/jankhunter/runtime/JankHunter" in owners)
        assertFalse("io/jankhunter/runtime/JankHunterHooks" in owners)
    }

    @Test fun okhttpEnabledRequiresTransportOwners() {
        val owners = HookAbiRequirements.requiredOwners(instrumentation(okhttp = true), autoInit = false)
        assertTrue("io/jankhunter/okhttp3/JankHunterHttpTransportV1" in owners)
        assertTrue("io/jankhunter/okhttp3/JankHunterOkHttp3" in owners)
    }

    @Test fun lifecycleLeaksAddsLifecycleOwners() {
        val owners = HookAbiRequirements.requiredOwners(instrumentation(lifecycleLeaks = true), autoInit = false)
        assertTrue("io/jankhunter/runtime/JankHunterLifecycleAccessorV1" in owners)
        assertTrue("io/jankhunter/runtime/JankHunterLifecycleTargetSinkV1" in owners)
    }

    @Test fun disabledInstrumentationRequiresNoOwners() {
        assertEquals(emptySet<String>(), HookAbiRequirements.requiredOwners(instrumentation(), autoInit = false))
    }
}
