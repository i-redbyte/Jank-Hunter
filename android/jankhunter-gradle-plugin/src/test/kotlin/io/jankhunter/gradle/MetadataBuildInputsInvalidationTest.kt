package io.jankhunter.gradle

import org.junit.Assert.assertNotEquals
import org.junit.Test

class MetadataBuildInputsInvalidationTest {
    @Test fun readerDescriptorIdentityIsAvailable() {
        val current = MetadataReaderCapabilities.identity
        assertNotEquals("", current)
    }

    @Test fun hookAbiRequiredOwnersChangeWithInstrumentation() {
        val minimal = HookAbiRequirements.requiredOwners(
            JankHunterResolvedInstrumentation(
                dependencyInjectionAnalysis = false,
                methodCounters = false,
                methodFilterMode = JankHunterMethodFilterMode.NONE,
                okhttp = false, webSockets = false, handlers = false,
                executors = false, threadLifecycle = false, concurrencyDiagnostics = false,
                coroutines = false, interactionOperations = false, lifecycleLeaks = false,
                logSpam = false, classGraph = false, runtimeCallGraph = false, composeTracing = false,
                roomTracing = false, databaseTracing = false, workerTracing = false,
                androidComponents = false, binderIPC = false, ioTracing = false,
                scope = JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES,
            ),
            autoInit = false,
        )
        val okhttp = HookAbiRequirements.requiredOwners(
            JankHunterResolvedInstrumentation(
                dependencyInjectionAnalysis = false,
                methodCounters = false,
                methodFilterMode = JankHunterMethodFilterMode.NONE,
                okhttp = true, webSockets = false, handlers = false,
                executors = false, threadLifecycle = false, concurrencyDiagnostics = false,
                coroutines = false, interactionOperations = false, lifecycleLeaks = false,
                logSpam = false, classGraph = false, runtimeCallGraph = false, composeTracing = false,
                roomTracing = false, databaseTracing = false, workerTracing = false,
                androidComponents = false, binderIPC = false, ioTracing = false,
                scope = JankHunterInstrumentationScope.NAMESPACE_AND_PACKAGES,
            ),
            autoInit = false,
        )
        assertNotEquals(minimal, okhttp)
    }
}
