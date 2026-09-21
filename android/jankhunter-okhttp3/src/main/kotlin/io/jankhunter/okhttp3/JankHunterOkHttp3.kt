package io.jankhunter.okhttp3

import io.jankhunter.runtime.JankHunterNetworkRuntime
import okhttp3.Call
import okhttp3.EventListener
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.WebSocketListener
import okhttp3.Response
import okhttp3.internal.http2.Header
import okhttp3.internal.http2.Http2Connection
import okhttp3.internal.http2.Http2Stream
import okio.BufferedSource
import okio.Okio
import okio.Source

/** Stateless fail-open ABI used by the Gradle instrumentation. */
object JankHunterOkHttp3 {
    private val fallbackClients = BoundedWeakIdentityCache<OkHttpClient, OkHttpClient>(MAX_FALLBACK_CLIENTS)
    private val http2Observation = ThreadLocal<Http2ObservationScope>()

    /** The connection owns this state; no global connection cache or additional socket reads. */
    @JvmStatic
    fun bufferHttpResponseSource(source: Source, owner: Any): BufferedSource {
        val observed = okHttpNonFatalOr(source) {
            if (owner !is JankHunterHttpTransportV1) source else HttpResponseByteSource(source).also {
                owner.jankHunterTransportState(it)
            }
        }
        return Okio.buffer(observed)
    }

    @JvmStatic
    fun bufferCachedResponseSource(source: Source, owner: Any): BufferedSource {
        val observed = okHttpNonFatalOr(source) {
            if (owner !is JankHunterHttpTransportV1) source else CachedResponseBodySource(source).also {
                owner.jankHunterTransportState(it)
            }
        }
        return Okio.buffer(observed)
    }

    @JvmStatic
    fun bindHttp2Transport(connection: Any, owner: Any) = okHttpNonFatal {
        val source = (owner as? JankHunterHttpTransportV1)?.jankHunterTransportState() as? HttpResponseByteSource
        if (connection is JankHunterHttpTransportV1 && source != null) {
            source.useHttp2()
            connection.jankHunterTransportState(source)
        }
    }

    /** Preserve OkHttp's original newStream result and exception, including cancellation. */
    @JvmStatic
    fun openObservedHttp2Stream(
        connection: Http2Connection,
        headers: List<Header>,
        hasBody: Boolean,
        listener: EventListener,
    ): Http2Stream {
        if (listener !is HttpTransportObservation) return connection.newStream(headers, hasBody)
        val scope = okHttpNonFatalOr<Http2ObservationScope?>(null) {
            http2Observation.get() ?: Http2ObservationScope().also { http2Observation.set(it) }
        } ?: return connection.newStream(headers, hasBody)
        val previous = scope.observation
        scope.observation = listener
        return try { connection.newStream(headers, hasBody) } finally { scope.observation = previous }
    }

    @JvmStatic
    fun onHttp2StreamAllocated(connection: Any, streamId: Int) = okHttpNonFatal {
        val observation = http2Observation.get()?.observation
        val source = (connection as? JankHunterHttpTransportV1)?.jankHunterTransportState() as? HttpResponseByteSource
        if (source != null) observation?.armTransport(source, streamId)
    }

    /** SDK completion only; it never synthesizes a callback on the application's listener. */
    @JvmStatic
    fun onHttpResponseReturned(response: Response, listener: EventListener) = okHttpNonFatal {
        (listener as? HttpTransportObservation)?.onFinalResponse(response)
    }

    private class Http2ObservationScope {
        var observation: HttpTransportObservation? = null
    }

    @JvmStatic
    fun wrapEventListenerFactory(factory: EventListener.Factory?): EventListener.Factory? {
        return okHttpNonFatalOr(factory) {
            if (factory is JankHunterEventListenerFactory) factory else JankHunterEventListenerFactory(factory)
        }
    }

    @JvmStatic
    fun installEventListenerFactory(builder: OkHttpClient.Builder?): OkHttpClient.Builder? {
        if (builder == null) return null
        return okHttpNonFatalOr(builder) {
            when (val lookup = eventListenerFactory(builder)) {
                is EventListenerFactoryLookup.Found -> {
                    val wrapped = wrapEventListenerFactory(lookup.factory)
                    if (wrapped != null) builder.eventListenerFactory(wrapped)
                }
                EventListenerFactoryLookup.Unavailable -> recordLookupFailure(null)
                is EventListenerFactoryLookup.Unsupported -> recordLookupFailure(lookup.reason)
            }
            builder
        }
    }

    /** Preserves an explicit application listener without allowing it to replace telemetry. */
    @JvmStatic
    fun installEventListener(
        builder: OkHttpClient.Builder,
        listener: EventListener,
    ): OkHttpClient.Builder {
        return okHttpNonFatalOrElse(
            block = {
                builder.eventListenerFactory(JankHunterEventListenerFactory(EventListener.Factory { listener }))
            },
            fallback = { okHttpNonFatalOr(builder) { builder.eventListener(listener) } },
        )
    }

    /**
     * Last-line invariant for clients built outside an instrumented builder call site. The normal
     * path is one type check; a weak cache pays client cloning and synchronization only once for an
     * unprotected client.
     */
    @JvmStatic
    fun newCall(client: OkHttpClient, request: Request): Call {
        val alreadyProtected = okHttpNonFatalOr(false) {
            client.eventListenerFactory() is JankHunterEventListenerFactory
        }
        val effectiveClient = if (alreadyProtected) {
            client
        } else {
            protectedClient(client)
        }
        return effectiveClient.newCall(request)
    }

    @JvmStatic
    fun wrapWebSocketListener(
        request: Request,
        listener: WebSocketListener?,
        ownerName: String?,
    ): WebSocketListener? {
        return okHttpNonFatalOr(listener) {
            if (listener == null || listener is JankHunterWebSocketListener ||
                !JankHunterNetworkRuntime.isWebSocketActive()
            ) {
                listener
            } else {
                val route = NetworkMetricNames.route(request.method(), request.url().encodedPath())
                JankHunterWebSocketListener(owner = ownerName, route = route, delegate = listener)
            }
        }
    }

    private fun recordLookupFailure(reason: String?) {
        okHttpNonFatal {
            JankHunterNetworkRuntime.counter("jankhunter.okhttp.event_listener_factory.lookup_failed.count", 1)
            if (reason != null) {
                JankHunterNetworkRuntime.counter("jankhunter.okhttp.event_listener_factory.$reason.count", 1)
            }
        }
    }

    private fun protectedClient(client: OkHttpClient): OkHttpClient {
        return okHttpNonFatalOr(client) {
            fallbackClients.getOrPut(client) { upgradeClient(client) }
        }
    }

    private fun upgradeClient(client: OkHttpClient): OkHttpClient {
        val builder = client.newBuilder()
        val originalFactory = when (val lookup = eventListenerFactory(builder)) {
            is EventListenerFactoryLookup.Found -> lookup.factory
            EventListenerFactoryLookup.Unavailable -> return client
            is EventListenerFactoryLookup.Unsupported -> {
                recordLookupFailure(lookup.reason)
                return client
            }
        }
        val wrapped = wrapEventListenerFactory(originalFactory) ?: return client
        val upgraded = builder.eventListenerFactory(wrapped).build()
        okHttpNonFatal {
            JankHunterNetworkRuntime.counter("jankhunter.okhttp.new_call_fallback_client.count", 1)
        }
        return upgraded
    }

    private fun eventListenerFactory(builder: OkHttpClient.Builder): EventListenerFactoryLookup {
        return okHttpNonFatalOr(EventListenerFactoryLookup.Unavailable) {
            val field = generateSequence<Class<*>>(builder.javaClass) { it.superclass }
                .flatMap { it.declaredFields.asSequence() }
                .firstOrNull { candidate ->
                    candidate.name == "eventListenerFactory" &&
                        EventListener.Factory::class.java.isAssignableFrom(candidate.type)
                }
                ?: return@okHttpNonFatalOr EventListenerFactoryLookup.Unsupported("unsupported_builder_layout")
            field.isAccessible = true
            val factory = field.get(builder)
                ?: return@okHttpNonFatalOr EventListenerFactoryLookup.Found(null)
            EventListenerFactoryLookup.Found(factory as EventListener.Factory)
        }
    }

    private sealed class EventListenerFactoryLookup {
        data class Found(val factory: EventListener.Factory?) : EventListenerFactoryLookup()
        data object Unavailable : EventListenerFactoryLookup()
        data class Unsupported(val reason: String) : EventListenerFactoryLookup()
    }

    private inline fun okHttpNonFatal(block: () -> Unit) {
        try {
            block()
        } catch (throwable: Throwable) {
            throwable.rethrowOkHttpFatal()
        }
    }

    private inline fun <T> okHttpNonFatalOr(fallback: T, block: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            throwable.rethrowOkHttpFatal()
            fallback
        }
    }

    private inline fun <T> okHttpNonFatalOrElse(block: () -> T, fallback: () -> T): T {
        return try {
            block()
        } catch (throwable: Throwable) {
            throwable.rethrowOkHttpFatal()
            fallback()
        }
    }

    private fun Throwable.rethrowOkHttpFatal() {
        if (this is VirtualMachineError || this is ThreadDeath) throw this
    }

    private const val MAX_FALLBACK_CLIENTS = 32
}
