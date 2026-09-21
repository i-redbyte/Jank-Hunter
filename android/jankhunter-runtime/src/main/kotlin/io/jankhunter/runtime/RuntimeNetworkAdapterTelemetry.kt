package io.jankhunter.runtime

import android.os.SystemClock
import io.jankhunter.runtime.internal.io.QualityCounterId

/** Process runtime port retained by optional network adapters, without exposing the full graph. */
internal class RuntimeNetworkAdapterTelemetry(
    private val access: RuntimeTelemetryAccess,
    private val context: RuntimeContextTelemetry,
    private val http: RuntimeHttpTelemetry,
    private val webSocket: RuntimeWebSocketTelemetry,
    private val system: RuntimeSystemTelemetry,
) {
    fun isActive(): Boolean = access.isActive()

    fun isHttpActive(): Boolean =
        access.isFeatureActive(JankHunterRuntimeFeature.HTTP)

    fun isWebSocketActive(): Boolean =
        access.isFeatureActive(JankHunterRuntimeFeature.WEBSOCKETS)

    fun captureContext(): JankHunterContextSnapshot = context.captureSnapshot()

    fun captureHttpContext(): JankHunterContextSnapshot {
        val epoch = access.collectionEpoch?.takeIf { isHttpActive() }
        val token = epoch?.tokens?.begin(RuntimeAsyncTokenTable.HTTP, SystemClock.elapsedRealtimeNanos()) ?: 0L
        return context.captureSnapshot(if (token == 0L) 0L else checkNotNull(epoch).id, token)
    }

    fun recordHttp(event: JankHunterHttpEvent) {
        val snapshot = event.contextSnapshot
        val epoch = access.collectionEpoch
        if (snapshot == null || snapshot.collectionEpochId == 0L) {
            access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_INVALID)
            return
        }
        if (epoch == null || snapshot.collectionEpochId != epoch.id) {
            access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_CLOSED)
            return
        }
        if (snapshot.httpToken != 0L) {
            if (access.claimAsync(epoch, snapshot.httpToken, RuntimeAsyncTokenTable.HTTP,
                    JankHunterRuntimeFeature.HTTP) < 0L) return
        } else {
            if (!snapshot.completeHttpOnce()) {
                access.rejectAsyncCompletion(RuntimeAsyncTokenTable.REJECT_CONSUMED)
                return
            }
            if (!isHttpActive()) {
                access.rejectAsyncCompletion(RuntimeCollectionEpochs.REJECT_FEATURE_DISABLED)
                return
            }
            epoch.writer.recordQuality(QualityCounterId.HTTP_LEGACY_CONTEXT_COMPLETION)
        }
        try {
            http.record(epoch.writer, event, epoch.config)
        } finally {
            epoch.tokens.release(snapshot.httpToken)
        }
    }

    fun recordWebSocket(event: JankHunterWebSocketEvent) {
        webSocket.record(access.writer ?: return, event)
    }

    fun counter(name: String, delta: Long) = system.recordCounter(name, delta)
}
