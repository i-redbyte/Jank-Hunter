package io.jankhunter.runtime

/** Fail-open stateless ABI dedicated to Android components and Binder bytecode hooks. */
internal object JankHunterAndroidHooks {
    @JvmStatic
    fun enterServiceCallback(): Long = guardedLong { hooks().enterServiceCallback() }

    @JvmStatic
    fun exitServiceCallback(
        token: Long,
        service: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
        resultCode: Int,
        resultObject: Any?,
        failed: Boolean,
    ) = guarded {
        hooks().exitServiceCallback(
            token,
            service,
            intent,
            componentId,
            componentName,
            stage,
            resultCode,
            resultObject,
            failed,
        )
    }

    @JvmStatic
    fun recordServiceForegroundTransition(
        service: Any?,
        componentId: Long,
        componentName: String,
        stage: Int,
    ) = guarded {
        hooks().recordServiceForegroundTransition(service, componentId, componentName, stage)
    }

    @JvmStatic
    fun enterReceiverCallback(
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
    ): Long = guardedLong {
        hooks().enterReceiverCallback(receiver, intent, componentId, componentName)
    }

    @JvmStatic
    fun registerReceiverAsync(
        pendingResult: Any?,
        receiver: Any?,
        intent: Any?,
        token: Long,
        componentId: Long,
        componentName: String,
    ): Boolean {
        return try {
            hooks().registerReceiverAsync(pendingResult, receiver, intent, token, componentId, componentName)
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            false
        }
    }

    @JvmStatic
    fun exitReceiverCallback(
        token: Long,
        receiver: Any?,
        intent: Any?,
        componentId: Long,
        componentName: String,
        asyncStarted: Boolean,
        pendingResult: Any?,
        failed: Boolean,
    ) = guarded {
        hooks().exitReceiverCallback(
            token,
            receiver,
            intent,
            componentId,
            componentName,
            asyncStarted,
            pendingResult,
            failed,
        )
    }

    @JvmStatic
    fun finishReceiverAsync(pendingResult: Any?) = guarded {
        hooks().finishReceiverAsync(pendingResult)
    }

    @JvmStatic
    fun enterBinderClient(): Long = guardedLong { hooks().enterBinderClient() }

    @JvmStatic
    fun exitBinderClient(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) = guarded {
        hooks().exitBinderClient(token, descriptor, method, code, flags, handled, throwable)
    }

    @JvmStatic
    fun enterBinderServer(): Long = guardedLong { hooks().enterBinderServer() }

    @JvmStatic
    fun exitBinderServer(
        token: Long,
        descriptor: String?,
        method: String?,
        code: Int,
        flags: Int,
        handled: Boolean,
        throwable: Throwable?,
    ) = guarded {
        hooks().exitBinderServer(token, descriptor, method, code, flags, handled, throwable)
    }

    private inline fun guarded(block: () -> Unit) {
        try {
            block()
        } catch (throwable: Throwable) {
            recordFailure(throwable)
        }
    }

    private inline fun guardedLong(block: () -> Long): Long {
        return try {
            block()
        } catch (throwable: Throwable) {
            recordFailure(throwable)
            0L
        }
    }

    private fun hooks(): RuntimeInstrumentationHooks = JankHunter.instrumentationHooks()

    private fun recordFailure(throwable: Throwable) {
        RuntimeHookGuard.rethrowFatal(throwable)
        RuntimeHookFailureTracker.record(RuntimeHookFailureReason.INSTRUMENTATION_HOOK)
    }
}
