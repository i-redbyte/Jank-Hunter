package io.jankhunter.workmanager

import android.os.Build
import androidx.work.WorkInfo
import androidx.work.Data
import io.jankhunter.runtime.JankHunterWorkConstraint
import io.jankhunter.runtime.JankHunterWorkDataField
import io.jankhunter.runtime.JankHunterWorkInfo
import io.jankhunter.runtime.JankHunterWorkState
import java.util.PriorityQueue

/** Public WorkInfo fields only. Class, unique names, dependencies and enqueue policy are not inferred from tags. */
internal fun workInfoSnapshot(info: WorkInfo, observedStopReason: Int?, instanceId: Long): JankHunterWorkInfo {
    val text = RegistryTextBudget()
    val constraints = info.constraints
    var mask = 0L
    if (constraints.requiresCharging()) mask = mask or JankHunterWorkConstraint.CHARGING.mask
    if (constraints.requiresBatteryNotLow()) mask = mask or JankHunterWorkConstraint.BATTERY_NOT_LOW.mask
    if (constraints.requiresStorageNotLow()) mask = mask or JankHunterWorkConstraint.STORAGE_NOT_LOW.mask
    if (constraints.requiresDeviceIdle()) mask = mask or JankHunterWorkConstraint.DEVICE_IDLE.mask
    val triggers = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.N) constraints.contentUriTriggers else emptySet()
    if (triggers.isNotEmpty()) mask = mask or JankHunterWorkConstraint.CONTENT_URI.mask
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P && constraints.requiredNetworkRequest != null) {
        mask = mask or JankHunterWorkConstraint.NETWORK_REQUEST.mask
    }
    val progress = info.progress.keyValueMap
    val output = info.outputData.keyValueMap
    return JankHunterWorkInfo(
        uuid = info.id.toString(), instanceId = instanceId, state = when (info.state) {
            WorkInfo.State.ENQUEUED -> JankHunterWorkState.ENQUEUED
            WorkInfo.State.RUNNING -> JankHunterWorkState.RUNNING
            WorkInfo.State.SUCCEEDED -> JankHunterWorkState.SUCCEEDED
            WorkInfo.State.FAILED -> JankHunterWorkState.FAILED
            WorkInfo.State.BLOCKED -> JankHunterWorkState.BLOCKED
            WorkInfo.State.CANCELLED -> JankHunterWorkState.CANCELLED
        },
        runAttempt = info.runAttemptCount, generation = info.generation,
        tags = smallestText(info.tags.asSequence(), MAX_FIELDS).map(text::bound),
        omittedTags = (info.tags.size - MAX_FIELDS).coerceAtLeast(0),
        initialDelayMs = info.initialDelayMillis,
        repeatIntervalMs = info.periodicityInfo?.repeatIntervalMillis, flexIntervalMs = info.periodicityInfo?.flexIntervalMillis,
        earliestEligibleUnixMs = info.nextScheduleTimeMillis.takeIf { info.state == WorkInfo.State.ENQUEUED && it != Long.MAX_VALUE },
        stopReason = observedStopReason, networkType = constraints.requiredNetworkType.name, constraintsMask = mask,
        contentTriggers = smallestText(triggers.asSequence().map {
            it.uri.toString() + if (it.isTriggeredForDescendants) " [descendants]" else ""
        }, MAX_TRIGGERS).map(text::bound),
        omittedContentTriggers = (triggers.size - MAX_TRIGGERS).coerceAtLeast(0),
        progress = metadata(progress, text), output = metadata(output, text),
        omittedProgressKeys = (progress.size - MAX_FIELDS).coerceAtLeast(0),
        omittedOutputKeys = (output.size - MAX_FIELDS).coerceAtLeast(0), truncatedFields = text.truncated,
    )
}

private fun metadata(values: Map<String, Any?>, text: RegistryTextBudget): List<JankHunterWorkDataField> =
    smallestText(values.keys.asSequence(), MAX_FIELDS).map { key ->
        val value = values[key]
        val (type, bytes) = dataMetadata(value)
        JankHunterWorkDataField(text.bound(key), type, bytes)
    }

internal data class ProgressMetadata(
    val fields: List<JankHunterWorkDataField>,
    val omittedKeys: Int,
    val truncatedFields: Int,
)

internal fun progressMetadata(data: Data): ProgressMetadata {
    val values = data.keyValueMap
    val text = RegistryTextBudget()
    return ProgressMetadata(metadata(values, text), (values.size - MAX_FIELDS).coerceAtLeast(0), text.truncated)
}

/** Keep only the lexicographically first fields; work metadata is never copied in full for sorting. */
private fun smallestText(values: Sequence<String>, limit: Int): List<String> {
    val selected = PriorityQueue<String>(limit, reverseOrder())
    values.forEach { value ->
        if (selected.size < limit) selected.add(value)
        else if (selected.peek()?.let { value < it } == true) {
            selected.remove()
            selected.add(value)
        }
    }
    return selected.toList().sorted()
}

private fun dataMetadata(value: Any?): Pair<String, Long?> = when (value) {
    null -> "null" to 0L
    is String -> "string" to value.toByteArray(Charsets.UTF_8).size.toLong()
    is Boolean -> "boolean" to 1L
    is Byte -> "byte" to 1L
    is Int -> "int" to 4L
    is Long -> "long" to 8L
    is Float -> "float" to 4L
    is Double -> "double" to 8L
    is Array<*> -> arrayMetadata(value)
    else -> "unknown" to null
}

private fun arrayMetadata(value: Array<*>): Pair<String, Long?> {
    val type = when (value.javaClass.componentType) {
        String::class.java -> "string"
        Boolean::class.javaObjectType -> "boolean"
        Byte::class.javaObjectType -> "byte"
        Int::class.javaObjectType -> "int"
        Long::class.javaObjectType -> "long"
        Float::class.javaObjectType -> "float"
        Double::class.javaObjectType -> "double"
        else -> return "unknown[]" to null
    }
    return "$type[]" to value.sumOf { dataMetadata(it).second ?: 0L }
}

private class RegistryTextBudget {
    var truncated = 0
        private set
    fun bound(value: String): String {
        if (value.length <= MAX_TEXT_CHARS) return value
        truncated++
        val end = if (value[MAX_TEXT_CHARS - 1].isHighSurrogate()) MAX_TEXT_CHARS - 1 else MAX_TEXT_CHARS
        return value.substring(0, end)
    }
}

private const val MAX_FIELDS = 8
private const val MAX_TRIGGERS = 4
private const val MAX_TEXT_CHARS = 128
