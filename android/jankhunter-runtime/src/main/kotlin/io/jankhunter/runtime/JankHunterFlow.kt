package io.jankhunter.runtime

class JankHunterFlow internal constructor(
    internal val previousFlow: String?,
    internal val previousStep: String?,
) : AutoCloseable {
    private var closed = false

    override fun close() {
        if (closed) return
        closed = true
        JankHunter.endFlow(this)
    }
}
