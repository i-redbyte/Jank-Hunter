package io.jankhunter.runtime.internal.io

import java.io.ByteArrayOutputStream
import java.io.DataOutputStream
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class HeapDumpCompletionValidatorTest {
    @Test fun acceptsCompletedSegmentedAndMonolithicDumpsAcrossSingleByteChunks() {
        assertTrue(validate(heapBytes(segmented = true)))
        assertTrue(validate(heapBytes(segmented = false)))
    }

    @Test fun rejectsEveryTruncatedPrefixAndTrailingPartialRecord() {
        val bytes = heapBytes(segmented = true)
        for (size in bytes.indices) assertFalse("prefix $size", validate(bytes.copyOf(size)))
        assertFalse(validate(bytes + byteArrayOf(1)))
    }

    @Test fun rejectsNameOnlyRandomHeaderOnlyAndInvalidEndRecords() {
        assertFalse(validate(byteArrayOf()))
        assertFalse(validate("hprof".toByteArray()))
        assertFalse(validate(ByteArray(4096)))
        assertFalse(validate(heapBytes(true).copyOf(31)))
        val invalid = heapBytes(true)
        invalid[invalid.lastIndex] = 1
        assertFalse(validate(invalid))
    }

    private fun validate(bytes: ByteArray): Boolean {
        val validator = HeapDumpCompletionValidator()
        for (index in bytes.indices) validator.accept(bytes, index, 1)
        return validator.isComplete()
    }

    private fun heapBytes(segmented: Boolean): ByteArray {
        val bytes = ByteArrayOutputStream()
        DataOutputStream(bytes).use { output ->
            output.write("JAVA PROFILE 1.0.3\u0000".toByteArray())
            output.writeInt(4)
            output.writeLong(0L)
            output.writeByte(if (segmented) 0x1c else 0x0c)
            output.writeInt(0)
            output.writeInt(5)
            output.writeByte(0xff)
            output.writeInt(1)
            if (segmented) {
                output.writeByte(0x2c)
                output.writeInt(0)
                output.writeInt(0)
            }
        }
        return bytes.toByteArray()
    }
}
