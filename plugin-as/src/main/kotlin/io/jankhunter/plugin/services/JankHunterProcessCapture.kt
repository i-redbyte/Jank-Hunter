package io.jankhunter.plugin.services

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.application.ApplicationManager
import java.io.InputStream
import java.nio.charset.StandardCharsets
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread

internal data class JankHunterCapturedProcess(
    val stdout: String,
    val stderr: String,
    val exitCode: Int?,
    val timedOut: Boolean,
) {
    val succeeded: Boolean
        get() = !timedOut && exitCode == 0

    fun combinedOutput(): String = buildString {
        append(stdout)
        append(stderr)
        if (timedOut) {
            if (isNotEmpty() && last() != '\n') append('\n')
            append("Process timed out.\n")
        }
    }
}

internal object JankHunterProcessCapture {
    fun run(
        commandLine: GeneralCommandLine,
        timeoutMillis: Long,
        maxCharsPerStream: Int = DEFAULT_MAX_CHARS_PER_STREAM,
    ): JankHunterCapturedProcess {
        check(!ApplicationManager.getApplication().isDispatchThread) {
            "External process capture must not run on the Swing event-dispatch thread"
        }
        require(timeoutMillis > 0) { "timeoutMillis must be positive" }
        require(maxCharsPerStream > 0) { "maxCharsPerStream must be positive" }

        val process = commandLine.createProcess()
        runCatching { process.outputStream.close() }

        val stdout = LimitedText(maxCharsPerStream)
        val stderr = LimitedText(maxCharsPerStream)
        val readers = listOf(
            drainAsync("jankhunter-stdout", process.inputStream, stdout),
            drainAsync("jankhunter-stderr", process.errorStream, stderr),
        )

        var finished = false
        var interrupted = false
        try {
            finished = process.waitFor(timeoutMillis, TimeUnit.MILLISECONDS)
            if (!finished) {
                terminate(process)
            }
        } catch (_: InterruptedException) {
            interrupted = true
            terminate(process)
        } finally {
            interrupted = joinPreservingInterrupt(readers) || interrupted
        }
        if (interrupted) Thread.currentThread().interrupt()

        return JankHunterCapturedProcess(
            stdout = stdout.value(),
            stderr = stderr.value(),
            exitCode = if (finished) runCatching { process.exitValue() }.getOrNull() else null,
            timedOut = !finished,
        )
    }

    fun terminate(process: Process) {
        var interrupted = Thread.interrupted()
        runCatching { process.outputStream.close() }
        runCatching { process.inputStream.close() }
        runCatching { process.errorStream.close() }
        process.destroy()
        val stopped = try {
            process.waitFor(GRACEFUL_STOP_TIMEOUT_MILLIS, TimeUnit.MILLISECONDS)
        } catch (_: InterruptedException) {
            interrupted = true
            false
        }
        if (!stopped) {
            process.destroyForcibly()
            try {
                process.waitFor(FORCIBLE_STOP_TIMEOUT_MILLIS, TimeUnit.MILLISECONDS)
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
    }

    private fun joinPreservingInterrupt(threads: List<Thread>): Boolean {
        var interrupted = Thread.interrupted()
        threads.forEach { reader ->
            try {
                reader.join(READER_JOIN_TIMEOUT_MILLIS)
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        return interrupted
    }

    private fun drainAsync(name: String, stream: InputStream, target: LimitedText): Thread =
        thread(name = name, start = true, isDaemon = true) {
            runCatching {
                stream.bufferedReader(StandardCharsets.UTF_8).use { reader ->
                    val buffer = CharArray(DEFAULT_BUFFER_SIZE)
                    while (true) {
                        val read = reader.read(buffer)
                        if (read < 0) break
                        target.append(buffer, read)
                    }
                }
            }
        }

    private class LimitedText(private val limit: Int) {
        private val text = StringBuilder(minOf(limit, DEFAULT_BUFFER_SIZE))
        private var truncated = false

        @Synchronized
        fun append(chars: CharArray, count: Int) {
            val available = limit - text.length
            if (available > 0) {
                text.append(chars, 0, minOf(available, count))
            }
            if (count > available) truncated = true
        }

        @Synchronized
        fun value(): String = buildString(text.length + TRUNCATED_SUFFIX.length) {
            append(text)
            if (truncated) {
                if (isNotEmpty() && last() != '\n') append('\n')
                append(TRUNCATED_SUFFIX)
            }
        }
    }

    private const val DEFAULT_MAX_CHARS_PER_STREAM = 256 * 1024
    private const val READER_JOIN_TIMEOUT_MILLIS = 1_000L
    private const val GRACEFUL_STOP_TIMEOUT_MILLIS = 500L
    private const val FORCIBLE_STOP_TIMEOUT_MILLIS = 1_000L
    private const val TRUNCATED_SUFFIX = "[process output truncated]\n"
}
