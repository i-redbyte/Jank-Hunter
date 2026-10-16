package io.jankhunter.gradle

import java.util.Properties
import java.util.concurrent.ConcurrentHashMap

/** A reader package can cover several metadata families; compiler patch versions are not selectors. */
internal data class MetadataReaderDescriptor(
    val id: String,
    val families: Set<String>,
    val kinds: Set<Int>,
    val knownFlags: Int,
    val protocol: Int,
    val priority: Int,
    val jars: Map<String, String>,
)

internal data class MetadataReaderSelection(
    val reader: MetadataReaderDescriptor?,
    val status: KotlinMetadataAnalysisStatus,
)

internal class MetadataReaderCapabilities(descriptors: List<MetadataReaderDescriptor>) {
    private val readers = descriptors.map {
        it.copy(families = java.util.Collections.unmodifiableSet(HashSet(it.families)),
            kinds = java.util.Collections.unmodifiableSet(HashSet(it.kinds)),
            jars = java.util.Collections.unmodifiableMap(java.util.TreeMap(it.jars)))
    }.sortedByDescending { it.priority }
    private val selections = ConcurrentHashMap<Header, MetadataReaderSelection>()

    init {
        require(readers.isNotEmpty() && readers.size <= 16)
        require(readers.map { it.id }.distinct().size == readers.size)
        readers.forEach { reader ->
            require(reader.id.matches(Regex("[a-zA-Z0-9_.-]+")))
            require(reader.protocol == BRIDGE_PROTOCOL)
            require(reader.families.isNotEmpty() && reader.families.all { it.matches(Regex("[1-9][0-9]*\\.[0-9]+")) })
            require(reader.kinds.isNotEmpty() && reader.kinds.all { it in 1..5 })
            require(reader.knownFlags in 0..255)
            require(reader.jars.isNotEmpty() && reader.jars.size <= 8)
            reader.jars.forEach { (name, digest) ->
                require(name.matches(Regex("[a-zA-Z0-9_.-]+\\.jar")) && !name.startsWith('.'))
                require(digest.matches(Regex("[a-f0-9]{64}")))
            }
        }
        readers.forEachIndexed { index, reader ->
            require(readers.drop(index + 1).none {
                it.priority == reader.priority && it.families.any(reader.families::contains) && it.kinds.any(reader.kinds::contains)
            }) { "Ambiguous metadata reader priority" }
        }
    }

    fun select(version: IntArray, kind: Int, flags: Int): MetadataReaderSelection {
        if (version.size !in 2..3 || version.any { it < 0 }) return unsupported(KotlinMetadataAnalysisStatus.UNREADABLE)
        if (kind !in 1..5) return unsupported(KotlinMetadataAnalysisStatus.UNSUPPORTED_KIND)
        val key = Header(version[0], version[1], kind, flags)
        selections[key]?.let {
            MetadataReaderMetrics.recordSelectorHit()
            return it
        }
        MetadataReaderMetrics.recordSelectorMiss()
        val selection = choose(key)
        synchronized(selections) {
            selections[key]?.let { return it }
            if (selections.size < 64) selections[key] = selection
        }
        return selection
    }

    private fun choose(header: Header): MetadataReaderSelection {
        val family = "${header.major}.${header.minor}"
        val familyReaders = readers.filter { family in it.families }
        if (familyReaders.isEmpty()) {
            MetadataReaderMetrics.recordUnsupportedSelection()
            return unsupported(KotlinMetadataAnalysisStatus.UNSUPPORTED_VERSION)
        }
        val kindReaders = familyReaders.filter { header.kind in it.kinds }
        if (kindReaders.isEmpty()) {
            MetadataReaderMetrics.recordUnsupportedSelection()
            return unsupported(KotlinMetadataAnalysisStatus.UNSUPPORTED_KIND)
        }
        // FIR's flag is valid only before metadata 2.0; public-inline ABI begins at 1.6.
        if ((header.flags and 64 != 0 && header.major >= 2) ||
            (header.flags and 128 != 0 && header.major == 1 && header.minor < 6)) {
            MetadataReaderMetrics.recordUnsupportedSelection()
            return unsupported(KotlinMetadataAnalysisStatus.UNSUPPORTED_FLAGS)
        }
        val reader = kindReaders.firstOrNull { header.flags and it.knownFlags.inv() == 0 }
            ?: run {
                MetadataReaderMetrics.recordUnsupportedSelection()
                return unsupported(KotlinMetadataAnalysisStatus.UNSUPPORTED_FLAGS)
            }
        return MetadataReaderSelection(reader, KotlinMetadataAnalysisStatus.READABLE)
    }

    private fun unsupported(status: KotlinMetadataAnalysisStatus) = MetadataReaderSelection(null, status)

    private data class Header(val major: Int, val minor: Int, val kind: Int, val flags: Int)

    companion object {
        const val BRIDGE_PROTOCOL = 1
        const val RESOURCE_PREFIX = "/META-INF/jankhunter/metadata-reader/"

        val identity: String by lazy {
            val bytes = checkNotNull(MetadataReaderCapabilities::class.java.getResourceAsStream(RESOURCE_PREFIX + "capabilities.properties"))
                .use { it.readBytes() }
            java.security.MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        }

        fun bundled(): MetadataReaderCapabilities {
            val properties = Properties()
            checkNotNull(MetadataReaderCapabilities::class.java.getResourceAsStream(RESOURCE_PREFIX + "capabilities.properties"))
                .use(properties::load)
            require(properties.getProperty("schema") == "1")
            val jars = properties.stringPropertyNames().filter { it.startsWith("jar.") }
                .associate { it.removePrefix("jar.") to properties.getProperty(it) }
            return MetadataReaderCapabilities(listOf(MetadataReaderDescriptor(
                properties.getProperty("reader.id"), properties.getProperty("metadata.families").split(',').toSet(),
                properties.getProperty("metadata.kinds").split(',').map(String::toInt).toSet(),
                properties.getProperty("metadata.flags").toInt(), properties.getProperty("bridge.protocol").toInt(),
                properties.getProperty("reader.priority").toInt(), jars,
            )))
        }
    }
}
