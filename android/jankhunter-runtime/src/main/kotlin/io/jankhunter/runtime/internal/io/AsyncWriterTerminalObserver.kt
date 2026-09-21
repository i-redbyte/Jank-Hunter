package io.jankhunter.runtime.internal.io

internal fun interface AsyncWriterTerminalObserver {
    fun onTerminal(writer: AsyncLogWriter, reason: Int, failure: Throwable?)

    companion object {
        val NONE = AsyncWriterTerminalObserver { _, _, _ -> }
    }
}
