package io.jankhunter.gradle

/** Consumes already-merged diagnostic lines; no classpath scan or unbounded class-name registry. */
internal class MetadataCompatibilitySummary {
    private val groups = LinkedHashMap<Key, Bucket>()
    private var incomplete = false
    private var truncated = false
    private var sampleBytes = 0
    private var observations = 0L
    private val sampleCounts = HashMap<Pair<String, String>, Int>()

    fun accept(line: String) {
        val match = METADATA.find(line) ?: return
        val status = match.groupValues[1].take(32)
        if (status != "READABLE" && status != "ABSENT") incomplete = true
        val requested = Key(status, match.groupValues[2].take(64), match.groupValues[3].take(128))
        val key = if (requested in groups || groups.size < 127) requested else OTHER.also { truncated = true }
        val bucket = groups.getOrPut(key) { Bucket() }
        bucket.count++; observations++
        val sampleKey = key.status to key.reader
        if (sampleCounts.getOrDefault(sampleKey, 0) >= 20) return
        val encodedName = CLASS_NAME.find(line)?.groupValues?.get(1) ?: return
        val sample = escapeJsonString(decodeName(encodedName))
        val bytes = sample.toByteArray(Charsets.UTF_8).size + 3
        if (sampleBytes + bytes > 448 * 1024) { truncated = true; return }
        sampleBytes += bytes
        bucket.samples.add(sample)
        sampleCounts[sampleKey] = sampleCounts.getOrDefault(sampleKey, 0) + 1
    }

    fun toJson(): String = buildString {
        append("{\"schema\":1,\"scope\":\"class_artifact_passes\",\"incomplete\":$incomplete,")
        append("\"truncated\":$truncated,\"observations\":$observations,\"groups\":[")
        groups.entries.forEachIndexed { index, (key, bucket) ->
            if (index > 0) append(',')
            append("{\"status\":\"${key.status}\",\"version\":\"${key.version}\",\"reader\":\"${key.reader}\",\"count\":${bucket.count},\"samples\":[")
            bucket.samples.forEachIndexed { sampleIndex, sample ->
                if (sampleIndex > 0) append(',')
                append('"'); append(sample); append('"')
            }
            append("]}")
        }
        append("]}\n")
    }.also { check(it.toByteArray(Charsets.UTF_8).size <= 512 * 1024) }

    private fun decodeName(encoded: String): String = buildString {
        var offset = 0
        while (offset < encoded.length && length < 2048) {
            val character = encoded[offset++]
            if (character != '\\' || offset >= encoded.length) { append(character); continue }
            when (val escaped = encoded[offset++]) {
                'n' -> append('\n')
                'r' -> append('\r')
                't' -> append('\t')
                'b' -> append('\b')
                'f' -> append('\u000c')
                'u' -> if (offset + 4 <= encoded.length) {
                    encoded.substring(offset, offset + 4).toIntOrNull(16)?.let { append(it.toChar()) }
                    offset += 4
                }
                else -> append(escaped)
            }
        }
    }

    private data class Key(val status: String, val version: String, val reader: String)
    private class Bucket(val samples: MutableList<String> = ArrayList(), var count: Long = 0)

    companion object {
        private val OTHER = Key("OTHER", "", "")
        private val METADATA = Regex("\"metadataStatus\":\"([A-Z_]+)\",\"metadataVersion\":\"([0-9.\\-]*)\",\"metadataReader\":\"([a-zA-Z0-9_.-]*)\"")
        private val CLASS_NAME = Regex("\"class\":\"((?:[^\"\\\\]|\\\\.)*)\"")
    }
}
