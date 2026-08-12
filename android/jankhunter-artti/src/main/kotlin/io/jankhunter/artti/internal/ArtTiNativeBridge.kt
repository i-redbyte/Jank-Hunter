package io.jankhunter.artti.internal

import java.nio.ByteBuffer

internal object ArtTiNativeBridge {
    @Volatile
    private var loaded = false

    @Synchronized
    fun loadLibrary(): Result<Unit> {
        if (loaded) return Result.success(Unit)
        return runCatching {
            System.loadLibrary(LIBRARY_NAME)
            loaded = true
        }
    }

    fun isLoaded(): Boolean = loaded

    external fun nativeHandshake(responseBuffer: ByteBuffer): Int

    external fun nativeInitialize(configBuffer: ByteBuffer): Int

    /** Returns encoded byte count or a negative [ArtTiNativeStatus] code. */
    external fun nativeDrain(outputBuffer: ByteBuffer, maxRecords: Int): Int

    external fun nativeStop(): Int

    /** Control-path refresh; never called from an ART callback. */
    external fun nativeRefreshThreadMetadata(): Int

    /** Control-path triggered capture for one selected Java thread. */
    external fun nativeCaptureStack(
        thread: Thread,
        trigger: Int,
        contextToken: Long,
        relatedSequence: Long,
    ): Int

    /** Resolves one process-local method ID into a bounded versioned direct buffer. */
    external fun nativeResolveMethod(methodId: Long, outputBuffer: ByteBuffer): Int

    /** Internal controlled-scenario producer; never called from an ART callback. */
    external fun nativePublishSynthetic(eventType: Int, count: Int): Int

    private const val LIBRARY_NAME = "jankhunter_artti"
}
