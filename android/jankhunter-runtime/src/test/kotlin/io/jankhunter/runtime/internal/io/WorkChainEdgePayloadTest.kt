package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class WorkChainEdgePayloadTest {
    @Test fun declaredEdgeCarriesBothExactUuidWordsAndObservedTime() {
        val payload = BinaryPayload()
        encodeWorkChainEdge(payload, WorkChainEdgeObservation(1, 2, 3, 4, 5, "x"))
        assertArrayEquals(byteArrayOf(1, 1, 2, 3, 4, 5, 0, 1, 120), payload.copyBytes())
    }

    @Test(expected = IllegalStateException::class)
    fun type34CannotEnterFiveBitMicroPage() {
        ColumnarMicroPage().append(Jhlog.TYPE_WORK_CHAIN_EDGE, 0, BinaryPayload(), null, null, 1)
    }
}
