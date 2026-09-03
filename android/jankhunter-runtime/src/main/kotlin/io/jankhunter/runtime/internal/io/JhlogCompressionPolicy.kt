package io.jankhunter.runtime.internal.io

/** Keeps section entropy coding mutually exclusive with outer DEFLATE. */
internal object JhlogCompressionPolicy {
    fun useRansSections(optionalFeatures: Long, chunkFlags: Int): Boolean {
        return optionalFeatures and Jhlog.FEATURE_RANS_MICRO_PAGE_SECTIONS != 0L &&
            chunkFlags and Jhlog.CHUNK_FLAG_GZIP == 0
    }
}
