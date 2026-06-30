package io.jankhunter.plugin.services

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessListener
import com.intellij.openapi.project.Project
import java.io.File
import java.nio.charset.StandardCharsets

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
        val adb = shellQuote(findAdb(project))
        val serial = if (deviceSerial.isBlank()) "" else "-s ${shellQuote(deviceSerial)} "
        val packageArg = shellQuote(packageName)
        val localArg = shellQuote(localDirectory.path)
        val command = "$adb ${serial}exec-out run-as $packageArg sh -c " +
            shellQuote("cd files/jankhunter 2>/dev/null && tar cf - .") +
            " | tar -xf - -C $localArg"
        startShell(command, onText) { ok ->
            val files = localDirectory
                .walkTopDown()
                .filter { it.isFile && it.extension.equals("jhlog", ignoreCase = true) }
                .sortedByDescending(File::lastModified)
                .toList()
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

    private fun startShell(command: String, onText: (String) -> Unit, onDone: (Boolean) -> Unit) {
        val commandLine = GeneralCommandLine("sh")
            .withParameters("-c", command)
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
            onText("ADB run-as error: ${error.message}\n")
            onDone(false)
        }
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

    private fun shellQuote(value: String): String =
        "'" + value.replace("'", "'\\''") + "'"
}
