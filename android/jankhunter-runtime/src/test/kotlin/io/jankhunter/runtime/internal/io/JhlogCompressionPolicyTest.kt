package io.jankhunter.runtime.internal.io

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class JhlogCompressionPolicyTest {
    @Test
    fun ransIsUsedOnlyForRawChunksThatDeclareIt() {
        assertTrue(
            JhlogCompressionPolicy.useRansSections(
                Jhlog.FEATURE_RANS_MICRO_PAGE_SECTIONS,
                chunkFlags = 0,
            ),
        )
        assertFalse(
            JhlogCompressionPolicy.useRansSections(
                Jhlog.FEATURE_RANS_MICRO_PAGE_SECTIONS,
                chunkFlags = Jhlog.CHUNK_FLAG_GZIP,
            ),
        )
        assertFalse(JhlogCompressionPolicy.useRansSections(Jhlog.OPTIONAL_FEATURES, chunkFlags = 0))
    }
}
