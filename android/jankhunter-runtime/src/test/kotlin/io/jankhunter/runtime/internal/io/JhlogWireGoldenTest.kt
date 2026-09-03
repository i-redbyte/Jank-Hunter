package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertArrayEquals
import org.junit.Test

class JhlogWireGoldenTest {
    @Test
    fun fileMagicMatchesSharedWireFixture() {
        assertArrayEquals(wireGolden("file-magic.bin"), Jhlog.FILE_MAGIC)
    }
}
