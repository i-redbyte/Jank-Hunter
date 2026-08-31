package io.jankhunter.runtime

internal fun interface RuntimeTaskExecutor {
    fun execute(task: () -> Unit): Boolean
}

internal fun interface RuntimeDelayedTaskExecutor {
    fun execute(delayMs: Long, task: () -> Unit): Boolean
}

internal fun interface RuntimeBlockingTaskExecutor {
    fun execute(timeoutMs: Long, task: () -> Unit): Boolean
}
