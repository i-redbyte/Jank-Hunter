package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterHttpEvent
import java.io.IOException
import okio.Buffer
import okio.Source
import okio.Timeout

/** The native cached ResponseBody owns this wrapper; native response identities are preserved. */
internal class CachedResponseBodySource(private val delegate: Source) : Source {
    private var terminal = false
    private var delivered = false
    private var failureKind = JankHunterHttpEvent.FAILURE_KIND_UNKNOWN
    private var observer: HttpTransportObservation? = null

    fun bind(observation: HttpTransportObservation) {
        val ready = synchronized(this) {
            if (delivered || observer != null) return
            if (terminal) {
                delivered = true
                true
            } else {
                observer = observation
                false
            }
        }
        if (ready) notify(observation)
    }

    @Synchronized
    fun detach(observation: HttpTransportObservation) {
        if (observer === observation) {
            observer = null
            delivered = true
        }
    }

    override fun read(sink: Buffer, byteCount: Long): Long = try {
        delegate.read(sink, byteCount).also { if (it < 0L) complete(JankHunterHttpEvent.FAILURE_KIND_UNKNOWN) }
    } catch (failure: IOException) {
        complete(JankHunterHttpEvent.FAILURE_KIND_IO)
        throw failure
    }

    override fun close() {
        try {
            delegate.close()
            complete(JankHunterHttpEvent.FAILURE_KIND_UNKNOWN)
        } catch (failure: IOException) {
            complete(JankHunterHttpEvent.FAILURE_KIND_IO)
            throw failure
        }
    }

    private fun complete(kind: Int) {
        val pending = synchronized(this) {
            if (terminal) return
            terminal = true
            failureKind = kind
            val pending = observer
            observer = null
            if (pending != null) delivered = true
            pending
        }
        if (pending != null) notify(pending)
    }

    private fun notify(observation: HttpTransportObservation) {
        try { observation.onCachedBodyComplete(failureKind) } catch (failure: Throwable) {
            if (failure is VirtualMachineError || failure is ThreadDeath) throw failure
        }
    }

    override fun timeout(): Timeout = delegate.timeout()
}
