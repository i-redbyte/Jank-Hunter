package io.jankhunter.runtime

import android.os.SystemClock
import java.io.File
import java.io.FileDescriptor
import java.nio.channels.FileChannel

/** Allocation-free call replacements emitted only for the reviewed high-level I/O allow-list. */
object JankHunterIOHooks {
    @JvmStatic
    fun readFileBytes(file: File, sourceId: Long, sourceName: String): ByteArray {
        if (!ioTelemetry().isEnabled()) return file.readBytes()
        val startedAt = SystemClock.elapsedRealtimeNanos()
        return try {
            file.readBytes().also { bytes ->
                record(JankHunterIOOperation.FILE_READ, startedAt, bytes.size.toLong(), JankHunterIOOutcome.SUCCESS, sourceId, sourceName)
            }
        } catch (failure: Throwable) {
            record(JankHunterIOOperation.FILE_READ, startedAt, UNKNOWN_BYTES, JankHunterIOOutcome.FAILURE, sourceId, sourceName)
            throw failure
        }
    }

    @JvmStatic
    fun writeFileBytes(file: File, bytes: ByteArray, sourceId: Long, sourceName: String) {
        if (!ioTelemetry().isEnabled()) {
            file.writeBytes(bytes)
            return
        }
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            file.writeBytes(bytes)
            record(JankHunterIOOperation.FILE_WRITE, startedAt, bytes.size.toLong(), JankHunterIOOutcome.SUCCESS, sourceId, sourceName)
        } catch (failure: Throwable) {
            record(JankHunterIOOperation.FILE_WRITE, startedAt, bytes.size.toLong(), JankHunterIOOutcome.FAILURE, sourceId, sourceName)
            throw failure
        }
    }

    @JvmStatic
    fun appendFileBytes(file: File, bytes: ByteArray, sourceId: Long, sourceName: String) {
        if (!ioTelemetry().isEnabled()) {
            file.appendBytes(bytes)
            return
        }
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            file.appendBytes(bytes)
            record(JankHunterIOOperation.FILE_WRITE, startedAt, bytes.size.toLong(), JankHunterIOOutcome.SUCCESS, sourceId, sourceName)
        } catch (failure: Throwable) {
            record(JankHunterIOOperation.FILE_WRITE, startedAt, bytes.size.toLong(), JankHunterIOOutcome.FAILURE, sourceId, sourceName)
            throw failure
        }
    }

    @JvmStatic
    fun syncFileDescriptor(descriptor: FileDescriptor, sourceId: Long, sourceName: String) {
        if (!ioTelemetry().isEnabled()) {
            descriptor.sync()
            return
        }
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            descriptor.sync()
            record(JankHunterIOOperation.FILE_SYNC, startedAt, UNKNOWN_BYTES, JankHunterIOOutcome.SUCCESS, sourceId, sourceName)
        } catch (failure: Throwable) {
            record(JankHunterIOOperation.FILE_SYNC, startedAt, UNKNOWN_BYTES, JankHunterIOOutcome.FAILURE, sourceId, sourceName)
            throw failure
        }
    }

    @JvmStatic
    fun forceFileChannel(channel: FileChannel, metadata: Boolean, sourceId: Long, sourceName: String) {
        if (!ioTelemetry().isEnabled()) {
            channel.force(metadata)
            return
        }
        val startedAt = SystemClock.elapsedRealtimeNanos()
        try {
            channel.force(metadata)
            record(JankHunterIOOperation.FILE_SYNC, startedAt, UNKNOWN_BYTES, JankHunterIOOutcome.SUCCESS, sourceId, sourceName)
        } catch (failure: Throwable) {
            record(JankHunterIOOperation.FILE_SYNC, startedAt, UNKNOWN_BYTES, JankHunterIOOutcome.FAILURE, sourceId, sourceName)
            throw failure
        }
    }

    private fun record(
        operation: JankHunterIOOperation,
        startedAt: Long,
        bytes: Long,
        outcome: JankHunterIOOutcome,
        sourceId: Long,
        sourceName: String,
    ) {
        ioTelemetry().recordAutomatic(
            operation,
            SystemClock.elapsedRealtimeNanos() - startedAt,
            bytes,
            outcome,
            sourceId,
            sourceName,
        )
    }

    private fun ioTelemetry(): RuntimeIOTelemetry = JankHunter.ioTelemetry()

    private const val UNKNOWN_BYTES = -1L
}
