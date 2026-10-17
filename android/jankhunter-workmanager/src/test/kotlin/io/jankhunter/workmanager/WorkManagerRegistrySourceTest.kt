package io.jankhunter.workmanager

import androidx.work.WorkInfo
import com.google.common.util.concurrent.ListenableFuture
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import java.util.UUID
import java.util.concurrent.CompletableFuture
import java.util.concurrent.Executor
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkManagerRegistrySourceTest {
    @Test
    fun conversionUsesSuppliedExecutorAndReportsTruncation() {
        val future = QueryFuture()
        val tasks = ArrayList<Runnable>()
        val values = ArrayList<JankHunterWorkRegistryResult>()
        val source = WorkManagerRegistrySource({ future }, maxJobs = 2)
        source.request(Executor { tasks.add(it) }, values::add)
        future.complete(List(3) { WorkInfo(UUID.randomUUID(), WorkInfo.State.ENQUEUED, emptySet()) })
        assertTrue(values.isEmpty())
        tasks.removeAt(0).run()
        val snapshot = values.single() as JankHunterWorkRegistryResult.Snapshot
        assertEquals(3, snapshot.totalObserved)
        assertEquals(2, snapshot.jobs.size)
    }

    @Test
    fun finishedHistoryCannotDisplaceCurrentRunningAndBlockedJobs() {
        val future = QueryFuture()
        val values = ArrayList<JankHunterWorkRegistryResult>()
        val running = WorkInfo(UUID.randomUUID(), WorkInfo.State.RUNNING, emptySet())
        val blocked = WorkInfo(UUID.randomUUID(), WorkInfo.State.BLOCKED, emptySet())
        val waiting = WorkInfo(UUID.randomUUID(), WorkInfo.State.ENQUEUED, emptySet())
        val old = List(150) { WorkInfo(UUID.randomUUID(), WorkInfo.State.SUCCEEDED, emptySet()) }
        WorkManagerRegistrySource({ future }, maxJobs = 3).request(Executor(Runnable::run), values::add)
        future.complete(old + blocked + waiting + running)
        val snapshot = values.single() as JankHunterWorkRegistryResult.Snapshot
        assertEquals(153, snapshot.totalObserved)
        assertEquals(listOf(running.id.toString(), blocked.id.toString(), waiting.id.toString()), snapshot.jobs.map { it.uuid })
    }

    @Test
    fun cappedFinishedHistoryRotatesAcrossPolls() {
        val finished = List(7) { WorkInfo(UUID.randomUUID(), WorkInfo.State.SUCCEEDED, emptySet()) }
        val seen = (0L..3L).flatMap { poll ->
            selectWorkRegistryRows(finished, limit = 2, poll = poll).map { it.id }
        }.toSet()
        assertEquals(finished.map { it.id }.toSet(), seen)
    }

    @Test
    fun closeCancelsTheFrameworkFuture() {
        val future = QueryFuture()
        WorkManagerRegistrySource({ future }, maxJobs = 2).request(Executor { }, { }).close()
        assertTrue(future.isCancelled)
    }

    private class QueryFuture : CompletableFuture<List<WorkInfo>>(), ListenableFuture<List<WorkInfo>> {
        override fun addListener(listener: Runnable, executor: Executor) {
            whenComplete { _, _ -> executor.execute(listener) }
        }
    }
}
