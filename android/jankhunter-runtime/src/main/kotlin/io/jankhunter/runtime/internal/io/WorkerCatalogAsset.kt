package io.jankhunter.runtime.internal.io

import java.io.ByteArrayOutputStream
import java.io.FileNotFoundException
import java.io.InputStream

internal const val WORKER_CATALOG_ASSET = "jankhunter/worker-catalog-v1.json"
internal const val MAX_WORKER_CATALOG_BYTES = 128 * 1024

/** Called once per collector epoch on maintenance; preserves build names without loading classes. */
internal fun readWorkerCatalogAsset(open: () -> InputStream): ByteArray = try {
    open().use { input ->
        val output = ByteArrayOutputStream()
        val buffer = ByteArray(4096)
        while (true) {
            val count = input.read(buffer)
            if (count < 0) break
            if (output.size() + count > MAX_WORKER_CATALOG_BYTES) return unavailableWorkerCatalog("oversized")
            output.write(buffer, 0, count)
        }
        output.toByteArray()
    }
} catch (_: FileNotFoundException) {
    unavailableWorkerCatalog("missing")
} catch (_: java.io.IOException) {
    unavailableWorkerCatalog("unreadable")
}

private fun unavailableWorkerCatalog(status: String) = "{\"schema\":1,\"status\":\"$status\"}".toByteArray(Charsets.UTF_8)

internal class PendingWorkerCatalog(private val document: ByteArray) : PendingLogEvent(Jhlog.TYPE_WORKER_CATALOG, null) {
    override fun writePayload(writer: BinaryLogWriter) {
        writer.emitSemantic(Jhlog.TYPE_WORKER_CATALOG, 0L, writer.payload().boundedBytes(document, MAX_WORKER_CATALOG_BYTES), null)
    }
}
