package io.jankhunter.workmanager

import android.os.Build
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.WorkQuery
import com.google.common.util.concurrent.ListenableFuture
import io.jankhunter.runtime.JankHunterWorkRegistryResult
import io.jankhunter.runtime.JankHunterWorkRegistrySource
import io.jankhunter.runtime.JankHunterWorkRegistryUnavailable
import java.util.concurrent.Executor
import java.util.concurrent.atomic.AtomicLong

/**
 * Uses WorkManager's supported query API, including finished rows still retained by WorkManager.
 * The adapter retains at most [maxJobs] converted rows. WorkManager itself materializes its query
 * result before this limit can be applied; its API does not offer pagination.
 */
class WorkManagerRegistrySource internal constructor(
    private val query: () -> ListenableFuture<List<WorkInfo>>,
    private val maxJobs: Int,
) : JankHunterWorkRegistrySource {
    private val poll = AtomicLong()
    constructor(manager: WorkManager, maxJobs: Int = 128) : this(
        { manager.getWorkInfos(WorkQuery.fromStates(WorkInfo.State.entries)) }, maxJobs,
    )

    init { require(maxJobs in 1..128) }

    override fun request(executor: Executor, receive: (JankHunterWorkRegistryResult) -> Unit): AutoCloseable {
        val future = query()
        try {
            future.addListener({
                val result = try {
                    val infos = future.get()
                    JankHunterWorkRegistryResult.Snapshot(
                        selectWorkRegistryRows(infos, maxJobs, poll.getAndIncrement()).map { info ->
                            workInfoSnapshot(
                                info,
                                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) info.stopReason else null,
                                WorkerTelemetry.instanceId(info.id),
                            )
                        },
                        infos.size,
                    )
                } catch (_: InterruptedException) {
                    Thread.currentThread().interrupt()
                    JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED)
                } catch (_: Exception) {
                    JankHunterWorkRegistryResult.Unavailable(JankHunterWorkRegistryUnavailable.QUERY_FAILED)
                }
                receive(result)
            }, executor)
        } catch (failure: Exception) {
            future.cancel(false)
            throw failure
        }
        return AutoCloseable { future.cancel(false) }
    }
}

/** Rotate each priority group so a capped snapshot eventually includes older and newer rows. */
internal fun selectWorkRegistryRows(infos: List<WorkInfo>, limit: Int, poll: Long = 0): List<WorkInfo> {
    val counts = IntArray(3)
    for (info in infos) {
        counts[info.state.priorityBucket()]++
    }
    val quotas = IntArray(3)
    var remaining = limit
    for (bucket in 0..2) {
        quotas[bucket] = minOf(remaining, counts[bucket])
        remaining -= quotas[bucket]
    }
    val offsets = IntArray(3) { bucket ->
        if (counts[bucket] == 0) 0 else ((poll % counts[bucket]) * quotas[bucket] % counts[bucket]).toInt()
    }
    val positions = IntArray(3)
    val selected = Array(3) { ArrayList<WorkInfo>(quotas[it]) }
    for (info in infos) {
        val bucket = info.state.priorityBucket()
        val position = positions[bucket]++
        val count = counts[bucket]
        val quota = quotas[bucket]
        val distance = position - offsets[bucket]
        if (quota > 0 && (if (distance < 0) distance + count else distance) < quota) selected[bucket].add(info)
    }
    val result = ArrayList<WorkInfo>(limit - remaining)
    selected.forEach(result::addAll)
    return result
}

private fun WorkInfo.State.priorityBucket(): Int = when (this) {
    WorkInfo.State.RUNNING -> 0
    WorkInfo.State.ENQUEUED, WorkInfo.State.BLOCKED -> 1
    WorkInfo.State.SUCCEEDED, WorkInfo.State.FAILED, WorkInfo.State.CANCELLED -> 2
}
