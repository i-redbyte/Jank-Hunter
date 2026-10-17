package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.JankHunterWorkDataField
import io.jankhunter.runtime.JankHunterWorkState
import io.jankhunter.runtime.JankHunterWorkInfo
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import java.util.UUID

/** Immutable bounded packets are prepared on maintenance, never on a WorkManager producer thread. */
internal fun encodeWorkRegistryPackets(
    snapshotId: Long,
    startedMs: Long,
    observedMs: Long,
    intervalMs: Long,
    result: JankHunterWorkRegistryResult,
): List<ByteArray> {
    require(snapshotId > 0 && startedMs >= 0 && observedMs >= startedMs && intervalMs > 0)
    return try {
        val snapshot = result as? JankHunterWorkRegistryResult.Snapshot
        val jobs = snapshot?.jobs.orEmpty()
        require(jobs.size <= MAX_JOBS && (snapshot == null || snapshot.totalObserved >= jobs.size))
        require(jobs.map { it.uuid }.distinct().size == jobs.size)
        val packets = ((jobs.size + ROWS_PER_PACKET - 1) / ROWS_PER_PACKET).coerceAtLeast(1)
        List(packets) { index ->
            val rows = jobs.subList(index * ROWS_PER_PACKET, minOf(jobs.size, (index + 1) * ROWS_PER_PACKET))
            val payload = BinaryPayload().uvarint(1).uvarint(snapshotId).uvarint(startedMs).uvarint(observedMs).uvarint(intervalMs)
                .uvarint(index.toLong()).uvarint(packets.toLong()).uvarint((snapshot?.totalObserved ?: 0).toLong())
                .uvarint(jobs.size.toLong()).uvarint((result as? JankHunterWorkRegistryResult.Unavailable)?.reason?.wireCode ?: 0)
                .uvarint(rows.size.toLong())
            rows.forEach { payload.workInfo(it) }
            check(payload.size <= MAX_PACKET_BYTES)
            payload.copyBytes()
        }
    } catch (_: Exception) {
        listOf(BinaryPayload().uvarint(1).uvarint(snapshotId).uvarint(startedMs).uvarint(observedMs).uvarint(intervalMs)
            .uvarint(0).uvarint(1).uvarint(0).uvarint(0)
            .uvarint(JankHunterWorkRegistryUnavailable.INVALID_SOURCE.wireCode).uvarint(0).copyBytes())
    }
}

private fun BinaryPayload.workInfo(row: JankHunterWorkInfo) {
    require(row.uuid.length == 36 && UUID.fromString(row.uuid).toString() == row.uuid)
    require(row.runAttempt >= 0 && row.generation >= 0 && row.initialDelayMs >= 0)
    require((row.repeatIntervalMs == null) == (row.flexIntervalMs == null))
    row.repeatIntervalMs?.let { require(it > 0 && row.flexIntervalMs!! in 0..it) }
    require(row.earliestEligibleUnixMs == null || row.state == JankHunterWorkState.ENQUEUED)
    require(row.constraintsMask and KNOWN_CONSTRAINTS.inv() == 0L)
    registryText(row.uuid, 36).uvarint(row.instanceId).uvarint(row.state.wireCode)
        .uvarint(row.runAttempt.toLong()).uvarint(row.generation.toLong()).uvarint(row.initialDelayMs)
    val presence = (if (row.repeatIntervalMs != null) HAS_PERIOD else 0L) or
        (if (row.earliestEligibleUnixMs != null) HAS_EARLIEST else 0L) or (if (row.stopReason != null) HAS_STOP else 0L)
    uvarint(presence)
    row.repeatIntervalMs?.let { uvarint(it).uvarint(row.flexIntervalMs!!) }
    row.earliestEligibleUnixMs?.let(::svarint)
    row.stopReason?.let { svarint(it.toLong()) }
    registryStrings(row.tags, MAX_FIELDS)
    registryCount(row.omittedTags)
    registryText(row.networkType, 64).uvarint(row.constraintsMask)
    registryStrings(row.contentTriggers, MAX_TRIGGERS)
    registryCount(row.omittedContentTriggers)
    workData(row.progress)
    workData(row.output)
    registryCount(row.omittedProgressKeys)
    registryCount(row.omittedOutputKeys)
    registryCount(row.truncatedFields)
}

internal fun BinaryPayload.workData(fields: List<JankHunterWorkDataField>) {
    require(fields.size <= MAX_FIELDS)
    uvarint(fields.size.toLong())
    for (field in fields) {
        registryText(field.key, MAX_TEXT_BYTES).registryText(field.type, 32)
        uvarint(if (field.payloadBytes == null) 0 else 1)
        field.payloadBytes?.let { require(it >= 0); uvarint(it) }
    }
}

private fun BinaryPayload.registryStrings(values: List<String>, maxCount: Int) {
    require(values.size <= maxCount)
    uvarint(values.size.toLong())
    values.forEach { registryText(it, MAX_TEXT_BYTES) }
}

private fun BinaryPayload.registryText(value: String, maxBytes: Int): BinaryPayload {
    require(value.length <= maxBytes && value.toByteArray(Charsets.UTF_8).size <= maxBytes)
    return boundedString(value, maxBytes)
}

private fun BinaryPayload.registryCount(value: Int) { require(value >= 0); uvarint(value.toLong()) }

private const val ROWS_PER_PACKET = 8
private const val MAX_JOBS = 128
private const val MAX_FIELDS = 8
private const val MAX_TRIGGERS = 4
private const val MAX_TEXT_BYTES = 512
private const val MAX_PACKET_BYTES = 128 * 1024
private const val KNOWN_CONSTRAINTS = 0x3fL
private const val HAS_PERIOD = 1L
private const val HAS_EARLIEST = 1L shl 1
private const val HAS_STOP = 1L shl 2

/** Every packet releases exactly once, including failed admission and terminal writer drain. */
internal class RegistryBatchReservation(
    packetCount: Int,
    private val slot: java.util.concurrent.atomic.AtomicReference<RegistryBatchReservation?>,
) {
    private val remaining = java.util.concurrent.atomic.AtomicInteger(packetCount)
    private val released = java.util.concurrent.atomic.AtomicIntegerArray(packetCount)

    fun release(index: Int) {
        if (released.compareAndSet(index, 0, 1) && remaining.decrementAndGet() == 0) slot.compareAndSet(this, null)
    }
}

internal class PendingWorkRegistry(
    private val bytes: ByteArray,
    private val reservation: RegistryBatchReservation,
    private val index: Int,
) : PendingLogEvent(Jhlog.TYPE_WORK_REGISTRY, null) {
    override fun recycle() = reservation.release(index)
    override fun rejectBeforeAdmission() = Unit
    override fun writePayload(writer: BinaryLogWriter) {
        writer.emitSemantic(Jhlog.TYPE_WORK_REGISTRY, 0, writer.payload().bytes(bytes, bytes.size), null)
    }
}
