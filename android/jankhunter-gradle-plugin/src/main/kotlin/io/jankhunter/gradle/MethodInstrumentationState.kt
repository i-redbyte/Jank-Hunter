package io.jankhunter.gradle

import org.objectweb.asm.Label

/** Mutable local-slot and exception-region state for one ASM method transformation. */
internal class MethodInstrumentationState {
    var runtimeCallStartLocal = UNALLOCATED_LOCAL
    var annotationScopeLocal = UNALLOCATED_LOCAL
    var annotationOperationLocal = UNALLOCATED_LOCAL
    var semanticStartLocal = UNALLOCATED_LOCAL
    var semanticOutcomeLocal = UNALLOCATED_LOCAL
    var semanticKind: SemanticHookKind? = null
    var databaseMethodStartLocal = UNALLOCATED_LOCAL
    var databaseMethodQueryLocal = UNALLOCATED_LOCAL
    var databaseMethodFingerprintLocal = UNALLOCATED_LOCAL
    var databaseMethodOperationLocal = UNALLOCATED_LOCAL
    var workerInstanceLocal = UNALLOCATED_LOCAL
    var workerRunAttemptLocal = UNALLOCATED_LOCAL
    var serviceCallbackStartLocal = UNALLOCATED_LOCAL
    var serviceResultCodeLocal = UNALLOCATED_LOCAL
    var serviceResultObjectLocal = UNALLOCATED_LOCAL
    var receiverCallbackStartLocal = UNALLOCATED_LOCAL
    var receiverAsyncStartedLocal = UNALLOCATED_LOCAL
    var receiverPendingResultLocal = UNALLOCATED_LOCAL
    var binderServerStartLocal = UNALLOCATED_LOCAL
    var binderServerResultLocal = UNALLOCATED_LOCAL
    val methodTryStart = Label()
    val methodTryEnd = Label()
    val methodExceptionHandler = Label()
    var currentLine: Int? = null
    var databaseInvocationOriginIndex = 0
    var constructorBodyEntered = false

    private companion object {
        const val UNALLOCATED_LOCAL = -1
    }
}
