package io.jankhunter.runtime.internal.io

import java.io.File
import java.io.RandomAccessFile
import java.util.concurrent.TimeUnit
import kotlin.system.exitProcess

internal object ExternalFileLockProbe {
    fun canAcquire(file: File): Boolean {
        val javaExecutable = File(System.getProperty("java.home"), "bin/java").absolutePath
        val classPath = listOf(
            codeSource(ExternalFileLockProbe::class.java),
            codeSource(Unit::class.java),
        ).distinct().joinToString(File.pathSeparator) { it.toURI().path }
        val process = ProcessBuilder(
            javaExecutable,
            "-cp",
            classPath,
            ExternalFileLockProbe::class.java.name,
            file.absolutePath,
        ).redirectErrorStream(true).start()
        check(process.waitFor(PROBE_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
            process.destroyForcibly()
            "External file-lock probe timed out"
        }
        val output = process.inputStream.bufferedReader().use { it.readText() }
        check(process.exitValue() == ACQUIRED || process.exitValue() == LOCKED) {
            "External file-lock probe failed (${process.exitValue()}): $output"
        }
        return process.exitValue() == ACQUIRED
    }

    private fun codeSource(type: Class<*>): java.net.URL =
        requireNotNull(requireNotNull(type.protectionDomain).codeSource).location

    @JvmStatic
    fun main(arguments: Array<String>) {
        val acquired = runCatching {
            RandomAccessFile(arguments.single(), "rw").use { access ->
                val lock = access.channel.tryLock() ?: return@use false
                lock.release()
                true
            }
        }.getOrElse {
            it.printStackTrace()
            exitProcess(ERROR)
        }
        exitProcess(if (acquired) ACQUIRED else LOCKED)
    }

    private const val ACQUIRED = 0
    private const val LOCKED = 2
    private const val ERROR = 3
    private const val PROBE_TIMEOUT_SECONDS = 10L
}
