package io.jankhunter.runtime

import android.content.Context
import android.content.ContextWrapper
import io.jankhunter.runtime.internal.io.CausalKind
import io.jankhunter.runtime.internal.io.CausalPhase
import java.io.File
import java.nio.file.Files
import java.util.concurrent.locks.ReentrantLock
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Test

class JankHunterResourcesTest {
    @Test fun applicationMonitorOnALeaseCannotBlockDiagnosticRelease() {
        val directory = Files.createTempDirectory("jh-resource-monitor-boundary").toFile()
        val held = java.util.concurrent.CountDownLatch(1)
        val release = java.util.concurrent.CountDownLatch(1)
        val done = java.util.concurrent.CountDownLatch(1)
        val failure = java.util.concurrent.atomic.AtomicReference<Throwable?>()
        var holder: Thread? = null
        var producer: Thread? = null
        try {
            JankHunter.init(TestContext(directory), JankHunterConfig.builder()
                .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
                .concurrencyDetailedEnabled(true).autoStartCollectors(false).logDirectory(directory).build())
            val telemetry = JankHunter.concurrencyTelemetry()
            val lease = checkNotNull(telemetry.beginResourceUse(1L, "fixture", 1, 1, null))
            holder = Thread { synchronized(lease) { held.countDown(); release.await() } }.also(Thread::start)
            assertTrue(held.await(5L, java.util.concurrent.TimeUnit.SECONDS))
            producer = Thread {
                try { assertTrue(telemetry.resourceReleased(lease)) }
                catch (error: Throwable) { failure.set(error) }
                finally { done.countDown() }
            }.also(Thread::start)
            assertTrue("resource hook waited for an application-owned monitor", done.await(100L, java.util.concurrent.TimeUnit.MILLISECONDS))
            assertEquals(null, failure.get())
        } finally {
            release.countDown(); holder?.join(5_000L); producer?.join(5_000L)
            JankHunter.shutdown(); directory.deleteRecursively()
        }
    }

    @Test fun useRecordsObservationAndCloseWithoutInventingAcquisition() {
        val directory = Files.createTempDirectory("jh-resource-use").toFile()
        try {
            JankHunter.init(
                TestContext(directory),
                JankHunterConfig.builder()
                    .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
                    .concurrencyDetailedEnabled(true)
                    .autoStartCollectors(false)
                    .logDirectory(directory)
                    .build(),
            )
            var closes = 0
            JankHunterResources.use(AutoCloseable { closes++ }) { Unit }
            assertEquals(1, closes)
            // Shutdown seals the writer asynchronously; flush is the deterministic read barrier.
            JankHunter.flush()
            assertEquals(
                listOf(CausalPhase.Resource.USE_ENTERED.toLong(), CausalPhase.Resource.RELEASED.toLong()),
                resourcePhases(directory),
            )
        } finally {
            JankHunter.shutdown()
            directory.deleteRecursively()
        }
    }

    @Test fun transferWithoutCurrentScopeKeepsTheBoundLeaseUntilExplicitTransfer() {
        val directory = Files.createTempDirectory("jh-resource-transfer").toFile()
        try {
            JankHunter.init(
                TestContext(directory),
                JankHunterConfig.builder()
                    .availableRuntimeFeatures(setOf(JankHunterRuntimeFeature.CONCURRENCY_DIAGNOSTICS))
                    .concurrencyDetailedEnabled(true)
                    .autoStartCollectors(false)
                    .logDirectory(directory)
                    .build(),
            )
            val telemetry = JankHunter.concurrencyTelemetry()
            val worker = Any()
            val scope = checkNotNull(telemetry.beginConcurrencyWorker(worker, 42L, 0, 0, 1L, 2L))
            val previous = telemetry.installConcurrencyContext(scope)
            val lease = try {
                checkNotNull(JankHunterResources.beginAcquire(JankHunterResourceKind.LOCK, true)).also {
                    JankHunterResources.acquired(it)
                }
            } finally {
                telemetry.restoreConcurrencyContext(previous)
            }
            assertTrue(lease.boundToScope)
            assertEquals(scope.entityKind, lease.scopeKind)
            assertEquals(scope.entityId, lease.scopeId)

            JankHunterResources.transferToCurrentScope(lease)
            assertTrue(lease.boundToScope)
            assertEquals(scope.entityKind, lease.scopeKind)
            assertEquals(scope.entityId, lease.scopeId)

            JankHunterResources.transferOutsideScope(lease)
            assertFalse(lease.boundToScope)
            assertEquals(0, lease.scopeKind)
            assertEquals(0L, lease.scopeId)
            val resumed = telemetry.installConcurrencyContext(scope)
            try {
                JankHunterResources.transferToCurrentScope(lease)
            } finally {
                telemetry.restoreConcurrencyContext(resumed)
            }
            assertTrue(lease.boundToScope)
            assertEquals(scope.entityKind, lease.scopeKind)
            assertEquals(scope.entityId, lease.scopeId)
            JankHunterResources.transferOutsideScope(lease)
            assertFalse(lease.boundToScope)
            JankHunterResources.released(lease)
            telemetry.endConcurrencyWorker(worker, scope, JankHunterWorkerOutcome.SUCCESS)
            JankHunter.flush()
            assertEquals(
                listOf(
                    CausalPhase.Resource.ACQUIRE_ATTEMPT.toLong(),
                    CausalPhase.Resource.ACQUIRED.toLong(),
                    CausalPhase.Resource.SCOPE_TRANSFERRED.toLong(),
                    CausalPhase.Resource.SCOPE_TRANSFERRED.toLong(),
                    CausalPhase.Resource.SCOPE_TRANSFERRED.toLong(),
                    CausalPhase.Resource.RELEASED.toLong(),
                ),
                resourcePhases(directory),
            )
            val transfers = CausalJhlogTestReader.read(directory).filter {
                it.kind == CausalKind.RESOURCE.toLong() &&
                    it.phase == CausalPhase.Resource.SCOPE_TRANSFERRED.toLong()
            }
            assertEquals(listOf(false, true, false), transfers.map { it.boundToScope })
            assertEquals(listOf(0L, scope.entityKind.toLong(), 0L), transfers.map { it.leaseScopeKind })
            assertEquals(listOf(0L, scope.entityId, 0L), transfers.map { it.leaseScopeId })
        } finally {
            JankHunter.shutdown()
            directory.deleteRecursively()
        }
    }

    @Test fun reentrantLockRetainsItsIdentityAndBalancesExceptionalExit() {
        val lock = ReentrantLock()
        val failure = IllegalStateException("application failure")
        try {
            JankHunterResources.withLock(lock) {
                assertEquals(1, lock.holdCount)
                JankHunterResources.withLock(lock) {
                    assertEquals(2, lock.holdCount)
                    throw failure
                }
            }
        } catch (actual: IllegalStateException) {
            assertSame(failure, actual)
        }
        assertFalse(lock.isLocked)
    }

    @Test fun monitorUsesTheOriginalObjectAndExitsOnFailure() {
        val monitor = Any()
        val failure = IllegalStateException("application failure")
        try {
            JankHunterResources.withMonitor(monitor) {
                assertTrue(Thread.holdsLock(monitor))
                throw failure
            }
        } catch (actual: IllegalStateException) {
            assertSame(failure, actual)
        }
        assertFalse(Thread.holdsLock(monitor))
    }

    @Test fun closeDoesNotReplaceThePrimaryFailure() {
        val primary = IllegalStateException("block")
        val closing = IllegalArgumentException("close")
        var closes = 0
        val resource = AutoCloseable { closes++; throw closing }
        try {
            JankHunterResources.use(resource) { original ->
                assertSame(resource, original)
                throw primary
            }
        } catch (actual: IllegalStateException) {
            assertSame(primary, actual)
            assertEquals(listOf(closing), actual.suppressed.toList())
        }
        assertEquals(1, closes)
    }

    @Test fun successfulBlockStillPropagatesCloseFailure() {
        val closing = IllegalArgumentException("close")
        try {
            JankHunterResources.use(AutoCloseable { throw closing }) { 42 }
            throw AssertionError("close failure was swallowed")
        } catch (actual: IllegalArgumentException) {
            assertSame(closing, actual)
        }
    }

    private class TestContext(private val filesDirectory: File) : ContextWrapper(null) {
        override fun getApplicationContext(): Context = this
        override fun getPackageName(): String = "com.example"
        override fun getFilesDir(): File = filesDirectory
        override fun getSystemService(name: String): Any? = null
    }

    private fun resourcePhases(directory: File): List<Long> =
        CausalJhlogTestReader.read(directory)
            .filter { it.kind == CausalKind.RESOURCE.toLong() }
            .map { it.phase }

}
