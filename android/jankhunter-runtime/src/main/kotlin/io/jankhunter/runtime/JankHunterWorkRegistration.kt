package io.jankhunter.runtime

/** An observation lease; closing it never cancels the application's work. */
interface JankHunterWorkRegistration : AutoCloseable {
    fun isActive(): Boolean
    fun observed(instanceId: Long, periodic: Boolean)
    override fun close()
}
