package io.jankhunter.gradle

import java.util.concurrent.atomic.AtomicInteger

/** Build-scoped counters for harnesses; hot path uses atomics only. */
internal enum class MetadataDecodePathway { MAIN, CAUSAL, LIFECYCLE }

internal object MetadataReaderMetrics {
    private val selectorHits = AtomicInteger()
    private val selectorMisses = AtomicInteger()
    private val unsupportedSelections = AtomicInteger()
    private val decodeAttempts = AtomicInteger()
    private val backendOpens = AtomicInteger()
    private val readerUnavailable = AtomicInteger()
    private val pathwayDecodes = java.util.EnumMap<MetadataDecodePathway, AtomicInteger>(MetadataDecodePathway::class.java)

    init {
        MetadataDecodePathway.entries.forEach { pathwayDecodes[it] = AtomicInteger() }
    }

    fun recordSelectorHit() = selectorHits.incrementAndGet()

    fun recordSelectorMiss() = selectorMisses.incrementAndGet()

    fun recordUnsupportedSelection() = unsupportedSelections.incrementAndGet()

    fun recordDecodeAttempt(pathway: MetadataDecodePathway = MetadataDecodePathway.MAIN) {
        decodeAttempts.incrementAndGet()
        pathwayDecodes.getValue(pathway).incrementAndGet()
    }

    fun pathwayDecodeCount(pathway: MetadataDecodePathway): Int = pathwayDecodes.getValue(pathway).get()

    fun recordBackendOpen() = backendOpens.incrementAndGet()

    fun recordReaderUnavailable() = readerUnavailable.incrementAndGet()

    fun reset() {
        selectorHits.set(0)
        selectorMisses.set(0)
        unsupportedSelections.set(0)
        decodeAttempts.set(0)
        backendOpens.set(0)
        readerUnavailable.set(0)
        pathwayDecodes.values.forEach { it.set(0) }
    }

    fun logLineOrNull(): String? {
        val decodes = decodeAttempts.get()
        if (decodes == 0 && backendOpens.get() == 0 && unsupportedSelections.get() == 0) return null
        return buildString {
            append("JH_METADATA_METRICS {")
            append("\"decode_count\":").append(decodes)
            append(",\"backend_open_count\":").append(backendOpens.get())
            append(",\"selector_hits\":").append(selectorHits.get())
            append(",\"selector_misses\":").append(selectorMisses.get())
            append(",\"unsupported_selections\":").append(unsupportedSelections.get())
            append(",\"reader_unavailable\":").append(readerUnavailable.get())
            append(",\"decode_main\":").append(pathwayDecodeCount(MetadataDecodePathway.MAIN))
            append(",\"decode_causal\":").append(pathwayDecodeCount(MetadataDecodePathway.CAUSAL))
            append(",\"decode_lifecycle\":").append(pathwayDecodeCount(MetadataDecodePathway.LIFECYCLE))
            append('}')
        }
    }
}
