package io.jankhunter.runtime.internal.system

/** Keeps unsupported counters distinct from a supported counter whose value is zero. */
internal data class UidTrafficSample(
    val uidPlusOne: Long,
    val rxBytes: Long,
    val txBytes: Long,
    val knownFlags: Int,
) {
    companion object {
        fun from(uid: Int, rxBytes: Long, txBytes: Long): UidTrafficSample {
            val uidKnown = uid >= 0
            val rxKnown = uidKnown && rxBytes >= 0L
            val txKnown = uidKnown && txBytes >= 0L
            return UidTrafficSample(
                uidPlusOne = if (uidKnown) uid.toLong() + 1L else 0L,
                rxBytes = rxBytes.coerceAtLeast(0L),
                txBytes = txBytes.coerceAtLeast(0L),
                knownFlags = (if (rxKnown) RX_KNOWN else 0) or (if (txKnown) TX_KNOWN else 0),
            )
        }

        const val RX_KNOWN = 1
        const val TX_KNOWN = 2
    }
}
