package io.jankhunter.runtime.internal.io

import io.jankhunter.runtime.internal.system.ProcThreadSnapshot

internal fun encodeThreadSnapshot(
    payload: BinaryPayload,
    snapshot: ProcThreadSnapshot,
    elapsedMs: Long,
    scanDurationMs: Long,
    intervalMs: Long,
    ticksPerSecond: Long,
) {
    require(snapshot.threads.size <= 2048)
    require(!snapshot.complete || snapshot.failedReads == 0 && snapshot.incompleteReason == null)
    // Reserve header/envelope space and the worst-case integer widths of each retained row.
    var budget = 240 * 1024 - 256
    var retained = 0
    for (row in snapshot.threads) {
        val javaBytes = row.java?.let {
            64 + minOf(it.name.toByteArray(Charsets.UTF_8).size, 1024) + minOf(it.source.toByteArray(Charsets.UTF_8).size, 1024)
        } ?: 1
        val estimate = 72 + minOf(row.name.toByteArray(Charsets.UTF_8).size, 1024) + javaBytes
        if (estimate > budget) break
        budget -= estimate
        retained++
    }
    val limited = retained < snapshot.threads.size
    val reason = if (limited) "payload_limit" else snapshot.incompleteReason.orEmpty()
    payload.uvarint(if (snapshot.javaIdentityCollection) 3 else 1)
        .uvarint(elapsedMs.coerceAtLeast(0)).uvarint(scanDurationMs.coerceAtLeast(0))
        .uvarint(intervalMs.coerceAtLeast(0)).uvarint(ticksPerSecond.coerceAtLeast(0))
        .uvarint(if (snapshot.complete && !limited) 1 else 0).uvarint(snapshot.failedReads.toLong())
        .boundedString(reason, 128).uvarint(retained.toLong())
    for (index in 0 until retained) {
        val row = snapshot.threads[index]
        payload.uvarint(row.tid).uvarint(row.startTicks).boundedString(row.name, 1024)
            .uvarint(row.osState.code.toLong()).uvarint(row.userTicks).uvarint(row.systemTicks)
            .svarint(row.priority).svarint(row.nice)
        if (snapshot.javaIdentityCollection) {
            val java = row.java
            payload.uvarint(if (java == null) 0 else 1)
            if (java != null) {
                payload.uvarint(java.id).uvarint(if (java.daemon) 1 else 0).uvarint(java.firstObservedElapsedNanos)
                    .boundedString(java.name, 1024).boundedString(java.state.name, 32).boundedString(java.source, 1024)
                    .uvarint(if (java.ownedByJankHunter) 1 else 0)
            }
        }
    }
    if (snapshot.javaIdentityCollection) payload.uvarint(snapshot.droppedJavaIdentities)
}

internal class PendingThreadSnapshot(
    private val snapshot: ProcThreadSnapshot,
    private val elapsedMs: Long,
    private val scanDurationMs: Long,
    private val intervalMs: Long,
    private val ticksPerSecond: Long,
    private val slot: java.util.concurrent.atomic.AtomicReference<Any?>,
    private val reservation: Any,
) : PendingLogEvent(Jhlog.TYPE_THREAD_SNAPSHOT, null) {
    override fun recycle() {
        slot.compareAndSet(reservation, null)
    }

    // enqueue may retry an unsuccessful offer while retaining the same reservation.
    override fun rejectBeforeAdmission() = Unit

    override fun writePayload(writer: BinaryLogWriter) {
        val payload = writer.payload()
        encodeThreadSnapshot(payload, snapshot, elapsedMs, scanDurationMs, intervalMs, ticksPerSecond)
        writer.emitSemantic(Jhlog.TYPE_THREAD_SNAPSHOT, 0L, payload, null)
    }
}
