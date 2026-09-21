package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter
import io.jankhunter.runtime.internal.system.RetainedHeapDumper

internal class RuntimeStorageValve(
    private val state: RuntimeState,
) {
    private var nextRequestId = 0L

    fun switchBinaryStorage(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long,
    ): JankHunterStorageSwitchResult {
        return synchronized(state.storageValveLock) storageSwitch@{
            val snapshot = synchronized(state.lifecycleLock) lifecycleSnapshot@{
                val configuration = state.configurationSnapshot()
                val config = configuration.config ?: return@lifecycleSnapshot null
                if (config.binaryStorage() === storage) {
                    return JankHunterStorageSwitchResult.ALREADY_ACTIVE
                }
                StorageSnapshot(
                    requestId = allocateRequestId(),
                    lifecycleGeneration = configuration.lifecycleGeneration,
                    writer = state.writer?.takeIf(AsyncLogWriter::isAcceptingEvents),
                    retainedHeapDumper = state.retainedHeapDumper,
                )
            } ?: return@storageSwitch JankHunterStorageSwitchResult.NOT_STARTED

            val activeWriter = snapshot.writer
            val result = if (activeWriter == null) {
                JankHunterStorageSwitchResult.SWITCHED
            } else {
                activeWriter.switchBinaryStorageBlocking(storage, timeoutMs) { finalResult ->
                    if (finalResult.isSuccessfulSwitch()) applyStorage(snapshot, storage)
                }
            }
            if (!result.isSuccessfulSwitch()) return@storageSwitch result

            if (applyStorage(snapshot, storage)) {
                result
            } else {
                staleSwitchResult()
            }
        }
    }

    private fun applyStorage(snapshot: StorageSnapshot, storage: JankHunterBinaryStorage?): Boolean {
        return synchronized(state.storageValveLock) {
            val current = state.configurationSnapshot()
            if (
                current.lifecycleGeneration == snapshot.lifecycleGeneration &&
                current.storageRequestId == snapshot.requestId &&
                current.selectedBinaryStorage === storage
            ) {
                return@synchronized true
            }
            if (!state.applyBinaryStorage(snapshot.lifecycleGeneration, snapshot.requestId, storage)) {
                return@synchronized false
            }
            snapshot.retainedHeapDumper?.switchBinaryStorage(storage)
            true
        }
    }

    private fun staleSwitchResult(): JankHunterStorageSwitchResult {
        return if (state.config == null) {
            JankHunterStorageSwitchResult.NOT_STARTED
        } else {
            JankHunterStorageSwitchResult.FAILED
        }
    }

    private fun allocateRequestId(): Long {
        nextRequestId++
        return nextRequestId
    }

    private class StorageSnapshot(
        val requestId: Long,
        val lifecycleGeneration: Long,
        val writer: AsyncLogWriter?,
        val retainedHeapDumper: RetainedHeapDumper?,
    )
}

private fun JankHunterStorageSwitchResult.isSuccessfulSwitch(): Boolean =
    this == JankHunterStorageSwitchResult.SWITCHED || this == JankHunterStorageSwitchResult.ALREADY_ACTIVE
