package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.AsyncLogWriter

internal class RuntimeStorageValve(
    private val state: RuntimeState,
    private val collectors: RuntimeCollectorService,
) {
    fun switchBinaryStorage(
        storage: JankHunterBinaryStorage?,
        timeoutMs: Long,
    ): JankHunterStorageSwitchResult {
        return synchronized(state.storageValveLock) storageSwitch@{
            val snapshot = synchronized(state.lifecycleLock) lifecycleSnapshot@{
                val config = state.config ?: return@lifecycleSnapshot null
                if (config.binaryStorage() === storage) {
                    return JankHunterStorageSwitchResult.ALREADY_ACTIVE
                }
                StorageSnapshot(config, state.writer?.takeIf(AsyncLogWriter::isAcceptingEvents))
            } ?: return@storageSwitch JankHunterStorageSwitchResult.NOT_STARTED

            val activeWriter = snapshot.writer
            val result = if (activeWriter == null) {
                JankHunterStorageSwitchResult.SWITCHED
            } else {
                activeWriter.switchBinaryStorageBlocking(storage, timeoutMs)
            }
            if (result != JankHunterStorageSwitchResult.SWITCHED &&
                result != JankHunterStorageSwitchResult.ALREADY_ACTIVE
            ) {
                return@storageSwitch result
            }

            val configurationUpdated = synchronized(state.lifecycleLock) updateConfiguration@{
                if (state.config !== snapshot.config) return@updateConfiguration false
                state.config = snapshot.config.toBuilder().binaryStorage(storage).build()
                state.selectedBinaryStorage = storage
                true
            }
            if (configurationUpdated) collectors.switchBinaryStorage(storage)
            result
        }
    }

    private class StorageSnapshot(
        val config: JankHunterConfig,
        val writer: AsyncLogWriter?,
    )
}
