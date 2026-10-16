package io.jankhunter.plugin.ui

/** Owns a background-created resource until the UI claims it or cancels the launch. */
internal class PendingResource<T : Any>(private val release: (T) -> Unit) : AutoCloseable {
    private var resource: T? = null
    private var closed = false

    fun publish(value: T): Boolean {
        val accepted = synchronized(this) {
            if (closed || resource != null) false else {
                resource = value
                true
            }
        }
        if (!accepted) release(value)
        return accepted
    }

    fun claim(): T? = synchronized(this) {
        if (closed) null else resource.also { resource = null; closed = true }
    }

    override fun close() {
        val abandoned = synchronized(this) {
            closed = true
            resource.also { resource = null }
        }
        abandoned?.let(release)
    }
}
