package io.jankhunter.runtime

import android.os.Looper
import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.io.BinaryLogWriter

internal class RuntimeTelemetryAccess(
    private val state: RuntimeState,
    private val contexts: ContextTracker,
    private val coordinator: RuntimeCoordinator,
    private val elapsedRealtimeMs: RuntimeLongSource,
    private val processImportance: RuntimeIntSource,
) {
    private val contextChanged: () -> Unit = { ensureContextRecorded() }

    @Volatile
    private var cachedProcessImportance = RuntimeProcessImportance.UNKNOWN

    @Volatile
    private var cachedAndroidProcessImportance = UNKNOWN_ANDROID_IMPORTANCE

    @Volatile
    private var processImportanceCheckedAtMs = Long.MIN_VALUE

    private val processImportanceLock = Any()

    val writer: AsyncLogWriter?
        get() = state.writer?.takeIf { it.isAcceptingEvents() }

    val config: JankHunterConfig?
        get() = state.config

    val collectionEpoch: RuntimeCollectionEpoch?
        get() = state.collectionEpochs.current

    fun epochForCompletion(token: Long): RuntimeCollectionEpoch? {
        if (token <= 0L) return null
        val epoch = collectionEpoch
        if (epoch == null) state.collectionEpochs.reject(RuntimeAsyncTokenTable.REJECT_CLOSED)
        return epoch
    }

    fun rejectAsyncCompletion(reason: Int) = state.collectionEpochs.reject(reason)

    fun claimAsync(epoch: RuntimeCollectionEpoch, token: Long, kind: Int, feature: JankHunterRuntimeFeature): Long {
        val started = epoch.tokens.claim(token, kind)
        if (started < 0L) return -1L
        if (!isFeatureActive(feature)) {
            epoch.tokens.release(token)
            rejectAsyncCompletion(RuntimeCollectionEpochs.REJECT_FEATURE_DISABLED)
            return -1L
        }
        return started
    }

    fun isActive(): Boolean = coordinator.isActiveForHooks()

    fun isFeatureActive(feature: JankHunterRuntimeFeature): Boolean = state.featureGate.isEnabled(feature)

    fun lifecycleGeneration(): Long = state.lifecycleGeneration

    fun captureContext(screenOverride: String? = null, ownerOverride: String? = null): JankHunterContext {
        return contexts.capture(screenOverride, ownerOverride)
    }

    fun currentOwnerOrNull(): String? = contexts.ownerOrNull()

    fun currentScreen(): String = contexts.currentScreen()

    fun <T> callWithOwner(ownerName: String?, block: () -> T): T {
        val context = RuntimeHookGuard.value<JankHunterContext?>(null) { captureContext() }
        return if (context == null) block() else callWithContext(context, ownerName, block)
    }

    inline fun <T> callWithContext(context: JankHunterContext, ownerName: String?, block: () -> T): T {
        var delegateStarted = false
        return try {
            contexts.callWithContext(context, ownerName, contextChanged) {
                delegateStarted = true
                block()
            }
        } catch (throwable: Throwable) {
            if (throwable is VirtualMachineError || throwable is ThreadDeath) throw throwable
            if (delegateStarted) throw throwable
            block()
        }
    }

    fun ensureContextRecorded(
        screenOverride: String? = null,
        ownerOverride: String? = null,
        activeWriter: AsyncLogWriter? = writer,
    ) {
        if (activeWriter == null) return
        val screen = contexts.capturedScreen(screenOverride)
        val owner = contexts.capturedOwner(ownerOverride)
        val operationId = contexts.currentOperationId()
        val mainLooper = Looper.getMainLooper()
        if (mainLooper != null && Looper.myLooper() === mainLooper) {
            val current = state.mainThreadContext
            if (current == null || current.screen != screen || current.owner != owner || current.operationId != operationId) {
                state.mainThreadContext = JankHunterContext(screen, owner, operationId)
            }
        }
        activeWriter.updateProducerContext(
            screen,
            owner,
            operationId,
        )
    }

    fun uiVisibility(): RuntimeUiVisibility = RuntimeUiVisibility.fromWire(state.uiVisibility.get())

    fun isUiVisible(): Boolean = uiVisibility() == RuntimeUiVisibility.VISIBLE

    fun processImportance(): RuntimeProcessImportance = refreshProcessImportance(forceRefresh = false)

    fun recordProcessState(writer: AsyncLogWriter, reason: Long, forceRefresh: Boolean) {
        val importance = refreshProcessImportance(forceRefresh)
        writer.processState(
            uiVisibility().wireValue.toLong(),
            importance.wireValue,
            cachedAndroidProcessImportance.coerceAtLeast(0).toLong(),
            reason,
        )
    }

    private fun refreshProcessImportance(forceRefresh: Boolean): RuntimeProcessImportance {
        val checkedAtMs = processImportanceCheckedAtMs
        val nowMs = elapsedRealtimeMs.getAsLong()
        if (!forceRefresh && checkedAtMs != Long.MIN_VALUE &&
            nowMs - checkedAtMs in 0 until PROCESS_FOREGROUND_CACHE_MS
        ) {
            return cachedProcessImportance
        }
        synchronized(processImportanceLock) {
            val refreshedAtMs = processImportanceCheckedAtMs
            if (!forceRefresh && refreshedAtMs != Long.MIN_VALUE &&
                nowMs - refreshedAtMs in 0 until PROCESS_FOREGROUND_CACHE_MS
            ) {
                return cachedProcessImportance
            }
            val androidImportance = RuntimeHookGuard.value(
                UNKNOWN_ANDROID_IMPORTANCE,
                RuntimeHookFailureReason.COLLECTOR,
            ) {
                processImportance.getAsInt()
            }
            val importance = RuntimeProcessImportance.fromAndroid(androidImportance)
            cachedProcessImportance = importance
            cachedAndroidProcessImportance = androidImportance
            processImportanceCheckedAtMs = nowMs
            return importance
        }
    }

    fun isUserRelevantForSampling(): Boolean {
        return isUiVisible() || processImportance().isUserRelevantForSampling
    }

    fun uiVisibleFlag(): Long {
        return if (isUiVisible()) BinaryLogWriter.FLAG_APP_FOREGROUND else 0L
    }

    private companion object {
        const val PROCESS_FOREGROUND_CACHE_MS = 1_000L
        const val UNKNOWN_ANDROID_IMPORTANCE = -1
    }
}
