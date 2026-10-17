package io.jankhunter.gradle

import org.objectweb.asm.Opcodes

internal data class WorkerCatalog(
    val classes: List<String>,
    val unresolvedClasses: Int,
    val omittedWorkers: Int,
)

/** Pre-R8 program inventory. No reflection, instantiation or changes to reachability. */
internal fun workerCatalog(index: LifecycleClassIndex, limit: Int): WorkerCatalog {
    require(limit >= 0)
    val classes = ArrayList<String>()
    var encodedBytes = 0
    var unresolved = 0
    var omitted = 0
    index.programClasses().filter { it.access and (Opcodes.ACC_ABSTRACT or Opcodes.ACC_INTERFACE) == 0 }.forEach { header ->
        when (workerKind(index, header.name)) {
            WorkerClassKind.WORKER -> {
                val name = header.name.replace('/', '.')
                val bytes = escapeJsonString(name).toByteArray(Charsets.UTF_8).size + 3
                if (classes.size < limit && bytes <= MAX_CLASS_BYTES && encodedBytes + bytes <= MAX_CLASS_LIST_BYTES) {
                    classes.add(name)
                    encodedBytes += bytes
                } else omitted++
            }
            WorkerClassKind.UNRESOLVED -> unresolved++
            WorkerClassKind.OTHER -> Unit
        }
    }
    return WorkerCatalog(classes.toList(), unresolved, omitted)
}

private enum class WorkerClassKind { WORKER, OTHER, UNRESOLVED }

private fun workerKind(index: LifecycleClassIndex, name: String): WorkerClassKind {
    val seen = HashSet<String>()
    var current: String? = name
    while (current != null) {
        if (!seen.add(current)) return WorkerClassKind.UNRESOLVED
        when (current) {
            "androidx/work/ListenableWorker", "androidx/work/Worker", "androidx/work/CoroutineWorker" -> return WorkerClassKind.WORKER
            "java/lang/Object" -> return WorkerClassKind.OTHER
        }
        val header = index.header(current) ?: return WorkerClassKind.UNRESOLVED
        current = header.superName
    }
    return WorkerClassKind.OTHER
}

internal fun WorkerCatalog.assetText(namespace: String): String = buildString {
    append("{\"schema\":1,\"status\":\"available\",\"scope\":\"application-and-packaged-dependencies-pre-r8\",\"namespace\":\"")
    append(escapeJsonString(namespace))
    append("\",\"dynamic_modules\":\"outside-base-scope\",\"unresolved_classes\":")
    append(unresolvedClasses)
    append(",\"omitted_workers\":")
    append(omittedWorkers)
    append(",\"classes\":[")
    classes.forEachIndexed { number, name ->
        if (number > 0) append(',')
        append('"').append(escapeJsonString(name)).append('"')
    }
    append("]}\n")
}

// Reserve 1 KiB for the schema, namespace and coverage counters in the 128 KiB asset.
private const val MAX_CLASS_LIST_BYTES = 127 * 1024
private const val MAX_CLASS_BYTES = 1027
