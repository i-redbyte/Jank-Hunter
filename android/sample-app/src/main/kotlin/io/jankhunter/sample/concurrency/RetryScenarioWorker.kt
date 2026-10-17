package io.jankhunter.sample.concurrency

import android.content.Context
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import io.jankhunter.workmanager.JankHunterCoroutineWorker
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Retry is deliberately followed by success, so it must not be presented as a failed chain. */
class RetryScenarioWorker(context: Context, parameters: WorkerParameters) : JankHunterCoroutineWorker(context, parameters) {
    override suspend fun doJankHunterWork(): Result {
        withContext(Dispatchers.IO) {
            File(applicationContext.cacheDir, "worker-attempt-$runAttemptCount.bin").writeBytes(ByteArray(1024))
        }
        return if (runAttemptCount == 0) Result.retry() else {
            Result.success(workDataOf("observed_attempt" to runAttemptCount))
        }
    }
}
