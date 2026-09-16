package io.jankhunter.gradle

internal object CoroutineStateMachineInstrumentationPolicy {
    fun matches(
        enabled: Boolean,
        methodName: String,
        methodDescriptor: String,
        classHierarchy: Set<String>,
    ): Boolean {
        return enabled && methodName == INVOKE_SUSPEND && methodDescriptor == INVOKE_SUSPEND_DESCRIPTOR &&
            classHierarchy.any { it in CONTINUATION_BASE_CLASSES }
    }

    private const val INVOKE_SUSPEND = "invokeSuspend"
    private const val INVOKE_SUSPEND_DESCRIPTOR = "(Ljava/lang/Object;)Ljava/lang/Object;"
    private val CONTINUATION_BASE_CLASSES = setOf(
        "kotlin/coroutines/jvm/internal/BaseContinuationImpl",
        "kotlin/coroutines/jvm/internal/ContinuationImpl",
        "kotlin/coroutines/jvm/internal/SuspendLambda",
    )
}
