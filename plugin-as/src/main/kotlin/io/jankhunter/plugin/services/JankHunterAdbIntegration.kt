package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import java.io.File
import java.io.InputStream
import java.nio.charset.StandardCharsets
import kotlin.concurrent.thread

data class JankHunterDevice(val serial: String, val label: String) {
    override fun toString(): String = label
}

object JankHunterAdbIntegration {
    fun findAdb(project: Project): String {
        val candidates = buildList {
            System.getenv("ANDROID_HOME")?.let { add(File(it, "platform-tools/adb").path) }
            System.getenv("ANDROID_SDK_ROOT")?.let { add(File(it, "platform-tools/adb").path) }
            project.basePath?.let { add(File(it, "android/local.properties").path) }
            System.getenv("PATH")?.split(File.pathSeparator)?.forEach { add(File(it, "adb").path) }
        }
        val localProperties = candidates.firstOrNull { it.endsWith("local.properties") }?.let(::File)
        if (localProperties?.isFile == true) {
            val sdkDir = localProperties.readLines()
                .firstOrNull { it.startsWith("sdk.dir=") }
                ?.substringAfter('=')
                ?.replace("\\:", ":")
            if (!sdkDir.isNullOrBlank()) {
                val adb = File(sdkDir, "platform-tools/adb")
                if (adb.canExecute()) return adb.path
            }
        }
        return candidates.firstOrNull { File(it).canExecute() } ?: "adb"
    }

    fun listDevices(project: Project): List<JankHunterDevice> {
        val output = runBlocking(project, listOf("devices", "-l"))
        return output.lines()
            .drop(1)
            .mapNotNull { line ->
                val trimmed = line.trim()
                if (trimmed.isBlank() || !trimmed.contains("device")) return@mapNotNull null
                val serial = trimmed.split(Regex("\\s+"), limit = 2).first()
                JankHunterDevice(serial, trimmed)
            }
    }

    fun pullLogs(
        project: Project,
        deviceSerial: String,
        remoteDirectory: String,
        localDirectory: File,
        onText: (String) -> Unit,
        onDone: (Boolean, List<File>) -> Unit,
    ) {
        localDirectory.mkdirs()
        val before = localDirectory.listFiles { file -> file.extension == "jhlog" }?.map { it.name to it.lastModified() }?.toMap().orEmpty()
        val args = mutableListOf<String>()
        if (deviceSerial.isNotBlank()) {
            args += listOf("-s", deviceSerial)
        }
        args += listOf("pull", remoteDirectory, localDirectory.path)
        start(project, args, onText) { ok ->
            val files = localDirectory
                .walkTopDown()
                .filter { it.isFile && it.extension == "jhlog" }
                .filter { before[it.name] != it.lastModified() || ok }
                .sortedByDescending(File::lastModified)
                .toList()
            onDone(ok, files)
        }
    }

    fun pullAppPrivateLogs(
        project: Project,
        deviceSerial: String,
        packageName: String,
        localDirectory: File,
        onText: (String) -> Unit,
        onDone: (Boolean, List<File>) -> Unit,
    ) {
        localDirectory.mkdirs()
        val before = snapshotJhlogs(localDirectory)
        ApplicationManager.getApplication().executeOnPooledThread {
            val ok = runCatching {
                pullRunAsTar(project, deviceSerial, packageName, localDirectory, onText)
            }.getOrElse { error ->
                onText("ADB run-as pull error: ${error.message.orEmpty()}\n")
                false
            }
            val files = localDirectory
                .walkTopDown()
                .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
                .filter { ok || before[relativeFileKey(localDirectory, it)] != it.lastModified() }
                .sortedByDescending(File::lastModified)
                .toList()
            if (ok && files.isEmpty()) {
                onText("ADB run-as pull succeeded, but no .jhlog files were found in ${localDirectory.path}.\n")
            }
            onDone(ok && files.isNotEmpty(), files)
        }
    }

    fun listAppPrivateLogs(project: Project, deviceSerial: String, packageName: String): String {
        val args = mutableListOf<String>()
        if (deviceSerial.isNotBlank()) args += listOf("-s", deviceSerial)
        args += listOf("shell", "run-as", packageName, "sh", "-c", "ls -la files/jankhunter 2>/dev/null")
        return runBlocking(project, args)
    }

    fun listRemoteLogs(project: Project, deviceSerial: String, remoteDirectory: String): String {
        val args = mutableListOf<String>()
        if (deviceSerial.isNotBlank()) args += listOf("-s", deviceSerial)
        args += listOf("shell", "ls", "-la", remoteDirectory)
        return runBlocking(project, args)
    }

    private fun start(project: Project, args: List<String>, onText: (String) -> Unit, onDone: (Boolean) -> Unit) {
        val commandLine = GeneralCommandLine(findAdb(project))
            .withParameters(args)
            .withCharset(StandardCharsets.UTF_8)
        try {
            val handler = OSProcessHandler(commandLine)
            handler.addProcessListener(
                object : ProcessListener {
                    override fun onTextAvailable(event: ProcessEvent, outputType: com.intellij.openapi.util.Key<*>) {
                        onText(event.text)
                    }

                    override fun processTerminated(event: ProcessEvent) {
                        onDone(event.exitCode == 0)
                    }
                },
            )
            handler.startNotify()
        } catch (error: ExecutionException) {
            onText("ADB error: ${error.message}\n")
            onDone(false)
        }
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
        val tarProcess = tarCommand.createProcess()
        val adbErr = StringBuilder()
        val tarOut = StringBuilder()
        val tarErr = StringBuilder()
        val readers = listOf(
            readTextAsync(adbProcess.errorStream, adbErr),
            readTextAsync(tarProcess.inputStream, tarOut),
            readTextAsync(tarProcess.errorStream, tarErr),
        )

        val copyError = runCatching {
            adbProcess.inputStream.use { input ->
                tarProcess.outputStream.use { output ->
                    input.copyTo(output)
                }
            }
        }.exceptionOrNull()
        if (copyError != null) {
            runCatching { adbProcess.destroy() }
        }

        val adbExit = adbProcess.waitFor()
        val tarExit = tarProcess.waitFor()
        readers.forEach { it.join() }

        appendIfNotBlank(onText, adbErr)
        appendIfNotBlank(onText, tarOut)
        appendIfNotBlank(onText, tarErr)
        if (copyError != null) {
            onText("ADB tar stream copy failed: ${copyError.message.orEmpty()}\n")
        }
        if (adbExit != 0) {
            onText(
                "ADB run-as exited with code $adbExit. Проверьте package, debuggable-сборку и наличие files/jankhunter.\n",
            )
        }
        if (tarExit != 0) {
            onText("Local tar exited with code $tarExit while unpacking run-as output.\n")
        }
        return adbExit == 0 && tarExit == 0 && copyError == null
    }

    private fun runBlocking(project: Project, args: List<String>): String =
        runCatching {
            val process = GeneralCommandLine(findAdb(project))
                .withParameters(args)
                .withCharset(StandardCharsets.UTF_8)
                .createProcess()
            val out = process.inputStream.bufferedReader().readText()
            val err = process.errorStream.bufferedReader().readText()
            process.waitFor()
            out + err
        }.getOrDefault("")

    private fun readTextAsync(stream: InputStream, target: StringBuilder): Thread =
        thread(start = true, isDaemon = true) {
            stream.bufferedReader(StandardCharsets.UTF_8).use { reader ->
                val buffer = CharArray(DEFAULT_BUFFER_SIZE)
                while (true) {
                    val read = reader.read(buffer)
                    if (read < 0) break
                    target.append(buffer, 0, read)
                }
            }
        }

    private fun appendIfNotBlank(onText: (String) -> Unit, text: StringBuilder) {
        val value = text.toString()
        if (value.isNotBlank()) onText(value)
    }

    private fun snapshotJhlogs(directory: File): Map<String, Long> =
        directory
            .walkTopDown()
            .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
            .associate { relativeFileKey(directory, it) to it.lastModified() }

    private fun relativeFileKey(root: File, file: File): String =
        runCatching { file.relativeTo(root).path }.getOrDefault(file.path)
}
