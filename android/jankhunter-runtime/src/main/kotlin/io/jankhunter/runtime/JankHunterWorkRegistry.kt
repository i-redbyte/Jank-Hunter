package io.jankhunter.runtime

import java.util.concurrent.Executor

/** Optional adapter port. Requests and result conversion run off the main thread. */
fun interface JankHunterWorkRegistrySource {
    /** Closing cancels this diagnostic request and is dispatched asynchronously by the SDK.
     * Implementations must not publish mutable application objects; late results are ignored after stop. */
    fun request(executor: Executor, receive: (JankHunterWorkRegistryResult) -> Unit): AutoCloseable
}

sealed interface JankHunterWorkRegistryResult {
    data class Snapshot(val jobs: List<JankHunterWorkInfo>, val totalObserved: Int) : JankHunterWorkRegistryResult
    data class Unavailable(val reason: JankHunterWorkRegistryUnavailable) : JankHunterWorkRegistryResult
}

/** WorkInfo observations are not enqueue/start events. Null fields are unavailable, never zero. */
data class JankHunterWorkInfo(
    val uuid: String,
    val instanceId: Long,
    val state: JankHunterWorkState,
    val runAttempt: Int,
    val generation: Int,
    val tags: List<String>,
    val omittedTags: Int,
    val initialDelayMs: Long,
    val repeatIntervalMs: Long?,
    val flexIntervalMs: Long?,
    val earliestEligibleUnixMs: Long?,
    val stopReason: Int?,
    val networkType: String,
    val constraintsMask: Long,
    val contentTriggers: List<String>,
    val omittedContentTriggers: Int,
    val progress: List<JankHunterWorkDataField>,
    val output: List<JankHunterWorkDataField>,
    val omittedProgressKeys: Int,
    val omittedOutputKeys: Int,
    val truncatedFields: Int,
)

enum class JankHunterWorkState(val wireCode: Long) {
    ENQUEUED(0), RUNNING(1), SUCCEEDED(2), FAILED(3), BLOCKED(4), CANCELLED(5),
}

/** Byte estimate counts UTF-8 or primitive payload bytes, excluding Data serialization overhead. */
data class JankHunterWorkDataField(val key: String, val type: String, val payloadBytes: Long?)

enum class JankHunterWorkConstraint(val mask: Long) {
    CHARGING(1L shl 0), BATTERY_NOT_LOW(1L shl 1), STORAGE_NOT_LOW(1L shl 2),
    DEVICE_IDLE(1L shl 3), CONTENT_URI(1L shl 4), NETWORK_REQUEST(1L shl 5),
}

enum class JankHunterWorkRegistryUnavailable(val wireCode: Long) {
    NO_SOURCE(1), QUERY_FAILED(2), QUERY_TIMEOUT(3), INVALID_SOURCE(4),
}
