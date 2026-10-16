package io.jankhunter.runtime.internal.io

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.os.Parcel
import android.os.Process
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import java.util.zip.CRC32
import java.util.zip.ZipFile
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HostStorageMultiprocessArtTest {
    @Test
    fun separateProcessesShareQuotaAndExportOneSessionAcrossChildReconfiguration() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val root = File(context.filesDir, "host-policy-multiprocess")
        val destination = File(context.filesDir, "host-policy-multiprocess-export")
        root.deleteRecursively()
        destination.deleteRecursively()
        val config = hostStorageProcessConfig(root)
        val recording = ProcessRecordingSession()
        val writer = AsyncLogWriterFactory(recording).open(root, config, "main", setOf("main", "remote"))
        writer.counter("multiprocess.parent", 3L)
        assertTrue(writer.flushBlocking(TIMEOUT_MS))
        val coordinator = ProcessLogSnapshotCoordinator.start(context, root, "main", writer::hostStorageSnapshot)
        val ready = CountDownLatch(1)
        val remote = AtomicReference<IBinder>()
        val connection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, service: IBinder?) {
                remote.set(service)
                ready.countDown()
            }
            override fun onServiceDisconnected(name: ComponentName?) = Unit
        }
        val bound = context.bindService(Intent(context, HostStorageProcessService::class.java), connection, Context.BIND_AUTO_CREATE)
        try {
            assertTrue("remote service binding failed", bound)
            assertTrue("remote process did not connect", ready.await(10, TimeUnit.SECONDS))
            val binder = checkNotNull(remote.get())
            val before = transact(binder, HostStorageProcessProtocol.START, root.absolutePath)
            assertTrue("test requires two OS processes", before.pid != Process.myPid())
            val after = transact(binder, HostStorageProcessProtocol.RECONFIGURE)
            assertEquals(before.pid, after.pid)
            assertEquals(before.path, after.path)
            assertEquals(2, ProcessSnapshotParticipants.active(root).size)
            val snapshot = checkNotNull(coordinator.capture()) { "cross-process coordinated snapshot failed" }
            snapshot.use {
                assertEquals(2, snapshot.processCount)
                assertEquals(2, snapshot.logPaths.size)
                assertEquals(2, snapshot.logPaths.map { File(it).parentFile!!.name }.toSet().size)
                assertEquals(1, snapshot.logPaths.map { File(it).parentFile!!.parentFile!!.name }.toSet().size)
                assertTrue(snapshot.logPaths.contains(before.path))
                val artifact = SessionArchiveExporter.exportArtifacts(root, destination, snapshot).single()
                assertFalse(artifact.containsCompletedHeapDump)
                ZipFile(artifact.archivePath).use { zip ->
                    val entries = zip.entries().asSequence().toList()
                    assertEquals(2, entries.size)
                    assertTrue(entries.all { it.name.endsWith(".jhlog") })
                    val buffer = ByteArray(4096)
                    entries.forEach { entry ->
                        val sourceIndex = snapshot.logPaths.indexOfFirst { it.endsWith(entry.name) }
                        assertTrue(sourceIndex >= 0)
                        assertEquals(snapshot.byteLimit(sourceIndex), entry.size)
                        val crc = CRC32()
                        zip.getInputStream(entry).use { input ->
                            while (true) {
                                val count = input.read(buffer)
                                if (count < 0) break
                                crc.update(buffer, 0, count)
                            }
                        }
                        assertEquals(entry.crc, crc.value)
                    }
                }
                assertTrue(writer.flushBlocking(TIMEOUT_MS))
                transact(binder, HostStorageProcessProtocol.FLUSH)
                assertSharedReservations(root, checkNotNull(config.storagePolicy()))
                val logs = root.walkTopDown().filter { it.isFile && it.extension == "jhlog" }.toList()
                assertEquals(2, logs.size)
                assertTrue(logs.all { it.length() <= HostStorageProcessProtocol.FILE_LIMIT_BYTES })
                assertTrue(SessionStorageBudget.physicalBytes(root) <= HostStorageProcessProtocol.TOTAL_LIMIT_BYTES)
                File(context.filesDir, "host-policy-multiprocess-evidence.json").writeText(
                    JSONObject().put("parentPid", Process.myPid()).put("childPid", before.pid)
                        .put("processCount", snapshot.processCount).put("childPathBefore", before.path)
                        .put("childPathAfter", after.path).put("archivePath", artifact.archivePath)
                        .put("physicalBytes", SessionStorageBudget.physicalBytes(root))
                        .put("quotaBytes", HostStorageProcessProtocol.TOTAL_LIMIT_BYTES)
                        .put("terminalReservationProcesses", 2).toString(),
                )
            }
        } finally {
            remote.get()?.let { runCatching { transact(it, HostStorageProcessProtocol.STOP) } }
            if (bound) context.unbindService(connection)
            coordinator.close()
            assertTrue(writer.close(TIMEOUT_MS))
            recording.close()
        }
    }

    private fun assertSharedReservations(root: File, policy: io.jankhunter.runtime.JankHunterStoragePolicy) {
        SessionArtifactReadLeases.acquire(root).use {
            SessionStorageBudget.open(root, policy).use { probe ->
                // Parent writer, remote writer and this probe each own a terminal reservation.
                val available = policy.archivesSizeLimitBytes - SessionStorageBudget.physicalBytes(root) -
                    3L * RunArchiveBudget.TERMINAL_RESERVE_BYTES
                assertTrue(available > 0L)
                probe.claim(available, terminal = false)
                try {
                    try {
                        probe.claim(1, terminal = false)
                        probe.releaseClaim(1)
                        throw AssertionError("remote writer reservation was not accounted in the shared quota")
                    } catch (expected: StorageBudgetExhaustedException) {
                        assertEquals(policy.archivesSizeLimitBytes, expected.limitBytes)
                    }
                } finally { probe.releaseClaim(available) }
            }
        }
    }

    private fun transact(binder: IBinder, code: Int, root: String? = null): RemoteState {
        val data = Parcel.obtain()
        val reply = Parcel.obtain()
        return try {
            data.writeInterfaceToken(HostStorageProcessProtocol.DESCRIPTOR)
            if (root != null) data.writeString(root)
            assertTrue(binder.transact(code, data, reply, 0))
            reply.readException()
            RemoteState(reply.readInt(), reply.readString())
        } finally { reply.recycle(); data.recycle() }
    }

    private data class RemoteState(val pid: Int, val path: String?)
    private companion object { const val TIMEOUT_MS = 5_000L }
}
