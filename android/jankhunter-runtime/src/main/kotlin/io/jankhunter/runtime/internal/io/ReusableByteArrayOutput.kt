package io.jankhunter.runtime.internal.io

import java.io.ByteArrayOutputStream

/** Growable output whose backing storage can be consumed synchronously without copying. */
internal class ReusableByteArrayOutput(initialCapacity: Int) : ByteArrayOutputStream(initialCapacity) {
    val buffer: ByteArray
        get() = buf

    val length: Int
        get() = count
}
