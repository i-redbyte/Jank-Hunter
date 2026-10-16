package io.jankhunter.runtime

import io.jankhunter.runtime.internal.io.WorkerFailureObservation

private const val MAX_FAILURE_FRAMES = 8
private const val MAX_FAILURE_STACK_BYTES = 1024

/** Cold-path capture: frame names and line numbers only; never exception messages or values. */
internal fun captureWorkerFailure(
    instanceId: Long,
    runAttempt: Int,
    generation: Int,
    observedNanos: Long,
    throwable: Throwable,
): WorkerFailureObservation {
    val exceptionClass = boundedUtf8(throwable.javaClass.name, 128)
    val frames = throwable.stackTrace
    val stack = StringBuilder()
    var truncated = frames.size > MAX_FAILURE_FRAMES
    for (index in 0 until minOf(frames.size, MAX_FAILURE_FRAMES)) {
        val frame = frames[index]
        val line = "${frame.className}.${frame.methodName}:${frame.lineNumber}"
        val candidate = if (stack.isEmpty()) line else "\n$line"
        val remaining = MAX_FAILURE_STACK_BYTES - stack.toString().toByteArray(Charsets.UTF_8).size
        if (candidate.toByteArray(Charsets.UTF_8).size > remaining) {
            truncated = true
            break
        }
        stack.append(candidate)
    }
    return WorkerFailureObservation(instanceId, runAttempt, generation, observedNanos,
        exceptionClass, stack.toString(), truncated)
}

private fun boundedUtf8(value: String, maxBytes: Int): String {
    var end = 0
    var bytes = 0
    while (end < value.length) {
        val codePoint = value.codePointAt(end)
        val nextBytes = when {
            codePoint <= 0x7f -> 1
            codePoint <= 0x7ff -> 2
            codePoint <= 0xffff -> 3
            else -> 4
        }
        if (bytes + nextBytes > maxBytes) break
        bytes += nextBytes
        end += Character.charCount(codePoint)
    }
    return value.substring(0, end)
}
