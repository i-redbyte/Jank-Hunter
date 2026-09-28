package io.jankhunter.runtime.internal.io

import android.app.Service
import android.content.Intent
import android.os.Binder
import android.os.IBinder
import android.os.Parcel
import android.os.Process
import io.jankhunter.runtime.JankHunterConfig
import io.jankhunter.runtime.JankHunterLogSnapshot
import io.jankhunter.runtime.JankHunterStoragePolicy
import java.io.File

/** Test APK only: owns a real writer and snapshot participant in a separate Linux process. */
class HostStorageProcessService : Service() {
    private val stateLock = Any()
    private val recording = ProcessRecordingSession()
    private val factory = AsyncLogWriterFactory(recording)
    private var writer: AsyncLogWriter? = null
    private var coordinator: ProcessLogSnapshotCoordinator? = null
    private var root: File? = null
    private val binder = object : Binder() {
        override fun onTransact(code: Int, data: Parcel, reply: Parcel?, flags: Int): Boolean {
            if (code !in HostStorageProcessProtocol.START..HostStorageProcessProtocol.STOP) {
                return super.onTransact(code, data, reply, flags)
            }
            data.enforceInterface(HostStorageProcessProtocol.DESCRIPTOR)
            val output = checkNotNull(reply)
            try {
                synchronized(stateLock) {
                    when (code) {
                        HostStorageProcessProtocol.START -> startRecording(File(checkNotNull(data.readString())))
                        HostStorageProcessProtocol.RECONFIGURE -> reconfigure()
                        HostStorageProcessProtocol.FLUSH -> check(checkNotNull(writer).flushBlocking(TIMEOUT_MS))
                        HostStorageProcessProtocol.STOP -> stopRecording()
                    }
                    output.writeNoException()
                    output.writeInt(Process.myPid())
                    output.writeString(writer?.awaitSessionProcessDirectory(TIMEOUT_MS)?.let { directory ->
                        directory.listFiles().orEmpty().single { it.extension == "jhlog" }.absolutePath
                    })
                }
            } catch (error: Exception) {
                output.writeException(error)
            }
            return true
        }
    }

    override fun onBind(intent: Intent?): IBinder = binder

    override fun onDestroy() {
        synchronized(stateLock) { stopRecording() }
        super.onDestroy()
    }

    private fun startRecording(directory: File) {
        check(writer == null)
        root = directory
        writer = factory.open(directory, hostStorageProcessConfig(directory), "remote", PROCESSES)
        checkNotNull(writer).counter("multiprocess.child.before", 1L)
        check(checkNotNull(writer).flushBlocking(TIMEOUT_MS))
        coordinator = ProcessLogSnapshotCoordinator.start(this, directory, "remote") { timeout ->
            synchronized(stateLock) { writer?.hostStorageSnapshot(timeout) }
        }
    }

    private fun reconfigure() {
        check(checkNotNull(writer).close(TIMEOUT_MS))
        val directory = checkNotNull(root)
        writer = factory.open(directory, hostStorageProcessConfig(directory, bufferSize = 2048), "remote", PROCESSES)
        checkNotNull(writer).counter("multiprocess.child.after", 2L)
        check(checkNotNull(writer).flushBlocking(TIMEOUT_MS))
    }

    private fun stopRecording() {
        coordinator?.close()
        coordinator = null
        writer?.let { check(it.close(TIMEOUT_MS)) }
        writer = null
        recording.close()
    }

    private companion object {
        const val TIMEOUT_MS = 5_000L
        val PROCESSES = setOf("main", "remote")
    }
}

internal object HostStorageProcessProtocol {
    const val DESCRIPTOR = "io.jankhunter.runtime.test.StorageProcess"
    const val START = IBinder.FIRST_CALL_TRANSACTION
    const val RECONFIGURE = START + 1
    const val FLUSH = START + 2
    const val STOP = START + 3
    const val FILE_LIMIT_BYTES = 128L * 1024L
    const val TOTAL_LIMIT_BYTES = 512L * 1024L
}

internal fun hostStorageProcessConfig(root: File, bufferSize: Int = 1024): JankHunterConfig =
    JankHunterConfig.builder().autoStartCollectors(false).mainProcessOnly(false).flushIntervalMs(60_000L)
        .logGrowthAnalyticsEnabled(false)
        .storagePolicy(JankHunterStoragePolicy(root, HostStorageProcessProtocol.FILE_LIMIT_BYTES,
            HostStorageProcessProtocol.TOTAL_LIMIT_BYTES, setOf("jhlog"), emptySet(), bufferSize, true))
        .build()

internal fun AsyncLogWriter.hostStorageSnapshot(timeoutMs: Long): JankHunterLogSnapshot? =
    captureSnapshotBlocking(timeoutMs)?.let { snapshot ->
        JankHunterLogSnapshot(snapshot.capturedAtMs, snapshot.logPaths, logByteLimits = snapshot.logByteLimits)
    }
