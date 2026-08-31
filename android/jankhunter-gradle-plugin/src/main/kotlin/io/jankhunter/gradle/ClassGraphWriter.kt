package io.jankhunter.gradle

internal data class ClassGraphEdgeKey(
    val caller: String,
    val calleeClass: String,
    val calleeMethod: String,
)

/** Serializes one deterministic class-graph shard for the instrumented class. */
internal object ClassGraphWriter {
    fun write(directoryPath: String, className: String, edges: Map<ClassGraphEdgeKey, Int>) {
        if (directoryPath.isBlank() || edges.isEmpty()) return
        InstrumentationArtifactFiles.writeClassShard(directoryPath, className, record(className.replace('/', '.'), edges))
    }

    fun isApplicationLike(owner: String): Boolean {
        return !InstrumentationPackages.isBuiltinExcluded(owner)
    }

    private fun record(className: String, edges: Map<ClassGraphEdgeKey, Int>): String {
        return buildString {
            append("{\"format\":")
            append(ArtifactSchemas.CLASS_GRAPH_FORMAT)
            append(",\"class\":\"")
            append(escapeJsonString(className))
            append("\",\"edges\":[")
            edges.entries.forEachIndexed { index, entry ->
                if (index > 0) append(',')
                append("{\"caller\":\"")
                append(escapeJsonString(entry.key.caller))
                append("\",\"calleeClass\":\"")
                append(escapeJsonString(entry.key.calleeClass))
                append("\",\"calleeMethod\":\"")
                append(escapeJsonString(entry.key.calleeMethod))
                append("\",\"count\":")
                append(entry.value)
                append('}')
            }
            append("]}\n")
        }
    }
}
