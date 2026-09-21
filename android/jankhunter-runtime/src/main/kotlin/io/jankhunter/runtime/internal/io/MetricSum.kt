package io.jankhunter.runtime.internal.io

/** Unsigned 128 / positive signed 64; gauge samples bound the quotient to Long.MAX_VALUE. */
internal fun roundedMetricAverage(low: Long, high: Long, count: Long): Long {
    if (count <= 0L) return 0L
    var quotient = 0L
    var remainder: Long
    if (high == 0L && low >= 0L) {
        quotient = low / count
        remainder = low % count
    } else {
        remainder = high
        for (bit in Long.SIZE_BITS - 1 downTo 0) {
            remainder = (remainder shl 1) or ((low ushr bit) and 1L)
            if (java.lang.Long.compareUnsigned(remainder, count) >= 0) {
                remainder -= count
                quotient = quotient or (1L shl bit)
            }
        }
    }
    val halfRoundedUp = count / 2L + count % 2L
    return if (remainder >= halfRoundedUp && quotient < Long.MAX_VALUE) quotient + 1L else quotient
}

internal fun metricSumAsDouble(low: Long, high: Long): Double =
    high.toDouble() * 18_446_744_073_709_551_616.0 + (low ushr 1).toDouble() * 2.0 + (low and 1L)
