package io.jankhunter.plugin.services

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import java.io.File
import java.io.InputStream
import java.nio.charset.StandardCharsets
import java.util.Properties
import java.util.concurrent.Future
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
import kotlin.concurrent.thread

data class JankHunterDevice(val serial: String, val label: String) {
    override fun toString(): String = label
}

private data class JankHunterPullResult(val succeeded: Boolean, val files: List<File>)

object JankHunterAdbIntegration {
    private fun findAdb(project: Project): String {
        val sdkCandidates = buildList {
            System.getenv("ANDROID_HOME")?.takeIf(String::isNotBlank)?.let(::add)
            System.getenv("ANDROID_SDK_ROOT")?.takeIf(String::isNotBlank)?.let(::add)
            project.basePath?.let { basePath ->
                sequenceOf(File(basePath, "local.properties"), File(basePath, "android/local.properties"))
                    .mapNotNull(::sdkDirectoryFrom)
                    .forEach(::add)
            }
        }
        sdkCandidates.forEach { sdkDirectory ->
            val adb = File(sdkDirectory, "platform-tools/adb")
            if (adb.isFile && adb.canExecute()) return adb.absolutePath
        }

        return System.getenv("PATH")
            ?.split(File.pathSeparator)
            ?.asSequence()
            ?.map { directory -> File(directory, "adb") }
            ?.firstOrNull { candidate -> candidate.isFile && candidate.canExecute() }
            ?.absolutePath
            ?: "adb"
    }

    /** Must be called away from the Swing event-dispatch thread. */
    fun listDevices(project: Project): List<JankHunterDevice> {
        val result = runCommand(project, listOf("devices", "-l"), ADB_QUERY_TIMEOUT_MILLIS)
        check(result.succeeded) { result.combinedOutput().trim().ifBlank { "adb devices failed" } }

        return result.stdout.lineSequence()
            .drop(1)
            .mapNotNull { line ->
                val fields = line.trim().split(WHITESPACE, limit = 3)
                if (fields.size < 2 || fields[1] != "device") return@mapNotNull null
                JankHunterDevice(fields[0], line.trim())
            }
            .toList()
    }

    fun pullAppPrivateLogs(
        project: Project,
        deviceSerial: String,
        packageName: String,
        localDirectory: File,
        onText: (String) -> Unit,
        onDone: (Boolean, List<File>) -> Unit,
    ): Future<*> {
        val started = AtomicBoolean(false)
        val completionDelivered = AtomicBoolean(false)
        val complete: (Boolean, List<File>) -> Unit = { succeeded, files ->
            if (completionDelivered.compareAndSet(false, true)) onDone(succeeded, files)
        }
        val task = object : FutureTask<Unit>(
            Runnable {
                started.set(true)
                try {
                    val result = pullAppPrivateLogsBlocking(
                        project,
                        deviceSerial,
                        packageName,
                        localDirectory,
                        onText,
                    )
                    complete(result.succeeded, result.files)
                } finally {
                    // Guarantees UI-state completion even if an unexpected plugin exception
                    // escapes before the normal result is assembled.
                    complete(false, emptyList())
                }
            },
            Unit,
        ) {
            override fun done() {
                // FutureTask.cancel() can win before the pooled worker starts. In that case the
                // runnable (and its finally block) never executes, so complete the callback here.
                if (isCancelled && !started.get()) complete(false, emptyList())
            }
        }
        try {
            ApplicationManager.getApplication().executeOnPooledThread(task)
        } catch (error: Exception) {
            onText("Cannot schedule ADB pull: ${error.message.orEmpty()}\n")
            task.cancel(false)
        }
        return task
    }

    private fun pullAppPrivateLogsBlocking(
        project: Project,
        deviceSerial: String,
        packageName: String,
        localDirectory: File,
        onText: (String) -> Unit,
    ): JankHunterPullResult {
        if (project.isDisposed) return JankHunterPullResult(false, emptyList())
        if (!localDirectory.exists() && !localDirectory.mkdirs()) {
            onText("Cannot create local log directory: ${localDirectory.path}\n")
            return JankHunterPullResult(false, emptyList())
        }

        val before = snapshotJhlogs(localDirectory)
        var interrupted = false
        val ok = try {
            pullRunAsTar(project, deviceSerial, packageName, localDirectory, onText)
        } catch (_: InterruptedException) {
            interrupted = true
            false
        } catch (error: Exception) {
            onText("ADB run-as pull error: ${error.message.orEmpty()}\n")
            false
        }
        if (interrupted || Thread.currentThread().isInterrupted) {
            onText("ADB run-as pull cancelled.\n")
            Thread.currentThread().interrupt()
            return JankHunterPullResult(false, emptyList())
        }
        val files = localDirectory
            .walkTopDown()
            .maxDepth(MAX_LOG_SEARCH_DEPTH)
            .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
            .filter { ok || before[relativeFileKey(localDirectory, it)] != it.lastModified() }
            .sortedByDescending(File::lastModified)
            .toList()
        if (ok && files.isEmpty()) {
            onText("ADB run-as pull succeeded, but no .jhlog files were found in ${localDirectory.path}.\n")
        }
        return JankHunterPullResult(ok && files.isNotEmpty(), files)
    }

    /** Must be called away from the Swing event-dispatch thread. */
    fun listAppPrivateLogs(project: Project, deviceSerial: String, packageName: String): String {
        val args = mutableListOf<String>()
        if (deviceSerial.isNotBlank()) args += listOf("-s", deviceSerial)
        args += listOf("shell", "run-as", packageName, "sh", "-c", "ls -la files/jankhunter 2>/dev/null")
        return runCommand(project, args, ADB_QUERY_TIMEOUT_MILLIS).combinedOutput()
    }

    private fun pullRunAsTar(
        project: Project,
        deviceSerial: String,
        packageName: String,
        localDirectory: File,
        onText: (String) -> Unit,
    ): Boolean {
        val adbArgs = mutableListOf<String>()
        if (deviceSerial.isNotBlank()) adbArgs += listOf("-s", deviceSerial)
        adbArgs += listOf(
            "exec-out",
            "run-as",
            packageName,
            "sh",
            "-c",
            "cd files/jankhunter 2>/dev/null && tar -cf - .",
        )
        val adbCommand = GeneralCommandLine(findAdb(project))
            .withParameters(adbArgs)
            .withCharset(StandardCharsets.UTF_8)
        val tarCommand = GeneralCommandLine("tar")
            .withParameters("-xf", "-", "-C", localDirectory.path)
            .withCharset(StandardCharsets.UTF_8)

        val adbProcess = adbCommand.createProcess()
        val tarProcess = try {
            tarCommand.createProcess()
        } catch (error: Exception) {
            JankHunterProcessCapture.terminate(adbProcess)
            throw error
        }

        val adbErr = LimitedText()
        val tarOut = LimitedText()
        val tarErr = LimitedText()
        val copyError = AtomicReference<Throwable?>()
        val workers = listOf(
            readTextAsync("jankhunter-adb-stderr", adbProcess.errorStream, adbErr),
            readTextAsync("jankhunter-tar-stdout", tarProcess.inputStream, tarOut),
            readTextAsync("jankhunter-tar-stderr", tarProcess.errorStream, tarErr),
            thread(name = "jankhunter-adb-tar-copy", start = true, isDaemon = true) {
                runCatching {
                    adbProcess.inputStream.use { input ->
                        tarProcess.outputStream.use { output -> input.copyTo(output) }
                    }
                }.onFailure(copyError::set)
            },
        )

        val deadlineNanos = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(ADB_PULL_TIMEOUT_MILLIS)
        var adbFinished = false
        var tarFinished = false
        try {
            adbFinished = waitUntil(adbProcess, deadlineNanos)
            if (adbFinished && copyError.get() == null) {
                tarFinished = waitUntil(tarProcess, deadlineNanos)
            }
        } finally {
            if (!adbFinished || !tarFinished || copyError.get() != null) {
                JankHunterProcessCapture.terminate(adbProcess)
                JankHunterProcessCapture.terminate(tarProcess)
            }
            joinWorkersPreservingInterrupt(workers)
        }

        appendIfNotBlank(onText, adbErr.value())
        appendIfNotBlank(onText, tarOut.value())
        appendIfNotBlank(onText, tarErr.value())

        val transferError = copyError.get()
        if (transferError != null) {
            onText("ADB tar stream copy failed: ${transferError.message.orEmpty()}\n")
        }
        if (!adbFinished || !tarFinished) {
            onText("ADB run-as pull timed out after ${ADB_PULL_TIMEOUT_MILLIS / 1_000} seconds.\n")
            return false
        }

        val adbExit = adbProcess.exitValue()
        val tarExit = tarProcess.exitValue()
        if (adbExit != 0) {
            onText(
                "ADB run-as exited with code $adbExit. Проверьте package, debuggable-сборку и наличие files/jankhunter.\n",
            )
        }
        if (tarExit != 0) {
            onText("Local tar exited with code $tarExit while unpacking run-as output.\n")
        }
        return adbExit == 0 && tarExit == 0 && transferError == null
    }

    private fun runCommand(project: Project, args: List<String>, timeoutMillis: Long): JankHunterCapturedProcess =
        runCatching {
            JankHunterProcessCapture.run(
                GeneralCommandLine(findAdb(project))
                    .withParameters(args)
                    .withCharset(StandardCharsets.UTF_8),
                timeoutMillis,
            )
        }.getOrElse { error ->
            JankHunterCapturedProcess("", "ADB error: ${error.message.orEmpty()}\n", null, timedOut = false)
        }

    private fun waitUntil(process: Process, deadlineNanos: Long): Boolean {
        val remainingNanos = deadlineNanos - System.nanoTime()
        if (remainingNanos <= 0) return false
        return process.waitFor(maxOf(1L, TimeUnit.NANOSECONDS.toMillis(remainingNanos)), TimeUnit.MILLISECONDS)
    }

    private fun joinWorkersPreservingInterrupt(workers: List<Thread>) {
        var interrupted = Thread.interrupted()
        workers.forEach { worker ->
            try {
                worker.join(WORKER_JOIN_TIMEOUT_MILLIS)
            } catch (_: InterruptedException) {
                interrupted = true
            }
        }
        if (interrupted) Thread.currentThread().interrupt()
    }

    private fun readTextAsync(name: String, stream: InputStream, target: LimitedText): Thread =
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

    private fun appendIfNotBlank(onText: (String) -> Unit, text: String) {
        if (text.isNotBlank()) onText(text)
    }

    private fun sdkDirectoryFrom(localProperties: File): String? {
        if (!localProperties.isFile) return null
        return runCatching {
            Properties().apply { localProperties.inputStream().use { input -> load(input) } }
                .getProperty("sdk.dir")
                ?.trim()
                ?.takeIf(String::isNotEmpty)
        }.getOrNull()
    }

    private fun snapshotJhlogs(directory: File): Map<String, Long> =
        directory
            .walkTopDown()
            .maxDepth(MAX_LOG_SEARCH_DEPTH)
            .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
            .take(MAX_TRACKED_LOGS)
            .associate { relativeFileKey(directory, it) to it.lastModified() }

    private fun relativeFileKey(root: File, file: File): String =
        runCatching { file.relativeTo(root).path }.getOrDefault(file.path)

    private class LimitedText {
        private val text = StringBuilder()
        private var truncated = false

        @Synchronized
        fun append(buffer: CharArray, count: Int) {
            val available = MAX_CAPTURE_CHARS - text.length
            if (available > 0) text.append(buffer, 0, minOf(available, count))
            if (count > available) truncated = true
        }

        @Synchronized
        fun value(): String = buildString {
            append(text)
            if (truncated) {
                if (isNotEmpty() && last() != '\n') append('\n')
                append("[process output truncated]\n")
            }
        }
    }

    private val WHITESPACE = Regex("\\s+")
    private const val ADB_QUERY_TIMEOUT_MILLIS = 10_000L
    private const val ADB_PULL_TIMEOUT_MILLIS = 60_000L
    private const val WORKER_JOIN_TIMEOUT_MILLIS = 1_000L
    private const val MAX_CAPTURE_CHARS = 256 * 1024
    private const val MAX_LOG_SEARCH_DEPTH = 8
    private const val MAX_TRACKED_LOGS = 200
}
