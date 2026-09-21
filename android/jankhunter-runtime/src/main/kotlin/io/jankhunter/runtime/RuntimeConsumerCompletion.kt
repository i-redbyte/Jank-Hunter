package io.jankhunter.runtime

/** A cold-path, once-only transfer of writer ownership from a stopped session to its consumer. */
internal class RuntimeConsumerCompletion {
    private val lock = Any()
    private var completed = false
    private var callback: (() -> Unit)? = null

    fun whenComplete(action: () -> Unit) {
        val runNow = synchronized(lock) {
            if (completed) true else {
                check(callback == null) { "Consumer already owns a completion callback" }
                callback = action
                false
            }
        }
        if (runNow) action()
    }

    fun complete() {
        val action = synchronized(lock) {
            if (completed) return
            completed = true
            callback.also { callback = null }
        }
        action?.invoke()
    }
}
