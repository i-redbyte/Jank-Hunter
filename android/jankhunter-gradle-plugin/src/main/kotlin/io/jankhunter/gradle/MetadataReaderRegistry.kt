package io.jankhunter.gradle

import java.lang.reflect.InvocationTargetException
import java.util.concurrent.locks.ReentrantReadWriteLock
import kotlin.concurrent.read
import kotlin.concurrent.write

internal interface MetadataReaderBackend : AutoCloseable {
    fun decode(metadata: kotlin.Metadata): Map<*, *>
}

internal data class MetadataReaderResult(
    val values: MetadataReaderPayload?,
    val status: KotlinMetadataAnalysisStatus,
    val readerId: String? = null,
)

internal data class MetadataReaderPayload(
    val generated: Map<String, String>,
    val declared: Set<String>,
    val delegates: Map<String, String?>,
    val known: Boolean,
    val synthetic: Boolean,
    val status: KotlinMetadataAnalysisStatus,
)

/** Shared read leases allow parallel decode; close cannot unload a backend still in use. */
internal class MetadataReaderRegistry(
    capabilitiesFactory: () -> MetadataReaderCapabilities,
    private val open: (MetadataReaderDescriptor) -> MetadataReaderBackend,
) : AutoCloseable {
    private val capabilities by lazy(capabilitiesFactory)
    private val lifecycle = ReentrantReadWriteLock()
    private val backends = HashMap<String, MetadataReaderBackend>()
    private val permanentFailures = HashSet<String>()
    private var closed = false

    fun read(metadata: kotlin.Metadata): MetadataReaderResult = lifecycle.read lease@{
        if (closed) return@lease unavailable()
        var selectedReader: String? = null
        try {
            val selection = capabilities.select(metadata.metadataVersion, metadata.kind, metadata.extraInt)
            val descriptor = selection.reader ?: return@lease MetadataReaderResult(null, selection.status)
            selectedReader = descriptor.id
            val backend = backend(descriptor) ?: return@lease unavailable(descriptor.id)
            val values = decodeBridge(backend.decode(metadata))
            MetadataReaderResult(values, values.status, descriptor.id)
        } catch (failure: Throwable) {
            val cause = (failure as? InvocationTargetException)?.targetException ?: failure
            rethrowFatal(cause)
            if (cause is InterruptedException) Thread.currentThread().interrupt()
            unavailable(selectedReader)
        }
    }

    private fun backend(descriptor: MetadataReaderDescriptor): MetadataReaderBackend? = synchronized(backends) {
        if (descriptor.id in permanentFailures) return null
        backends[descriptor.id]?.let { return it }
        if (backends.size >= 8) error("Metadata reader session limit exceeded")
        return try {
            MetadataReaderMetrics.recordBackendOpen()
            open(descriptor).also { backends[descriptor.id] = it }
        } catch (failure: Throwable) {
            rethrowFatal(failure)
            if (failure is InterruptedException) Thread.currentThread().interrupt()
            if (failure is PermanentMetadataReaderFailure) permanentFailures += descriptor.id
            null
        }
    }

    private fun decodeBridge(values: Map<*, *>): MetadataReaderPayload {
        check(values["protocol"] == MetadataReaderCapabilities.BRIDGE_PROTOCOL) { "Metadata bridge protocol mismatch" }
        check(values["status"] is String && values["known"] is Boolean && values["synthetic"] is Boolean)
        val generated = values["generated"] as? Map<*, *> ?: error("Missing generated index")
        val declared = values["declared"] as? Set<*> ?: error("Missing declared index")
        val delegates = values["delegates"] as? Map<*, *> ?: error("Missing delegated index")
        val status = KotlinMetadataAnalysisStatus.valueOf(values["status"] as String)
        check((status == KotlinMetadataAnalysisStatus.READABLE) == (values["known"] == true))
        // Validate while making the host-owned copy, instead of traversing every index twice.
        return MetadataReaderPayload(generated.entries.associate { (it.key as String) to (it.value as String) },
            declared.map { it as String }.toSet(),
            delegates.entries.associate { (it.key as String) to (it.value as String?) },
            values["known"] == true, values["synthetic"] == true, status)
    }

    override fun close(): Unit = lifecycle.write {
        if (closed) return@write
        closed = true
        var firstFailure: Throwable? = null
        backends.values.forEach { backend ->
            try { backend.close() }
            catch (failure: Throwable) {
                rethrowFatal(failure)
                if (firstFailure == null) firstFailure = failure else firstFailure?.addSuppressed(failure)
            }
        }
        backends.clear()
        permanentFailures.clear()
        firstFailure?.let { throw it }
    }

    private fun unavailable(readerId: String? = null): MetadataReaderResult {
        MetadataReaderMetrics.recordReaderUnavailable()
        return MetadataReaderResult(null, KotlinMetadataAnalysisStatus.READER_UNAVAILABLE, readerId)
    }

    private fun rethrowFatal(failure: Throwable) {
        if (failure is VirtualMachineError || failure is ThreadDeath) throw failure
    }
}
