package io.jankhunter.gradle

import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.File
import org.h2.mvstore.MVMap
import org.h2.mvstore.MVStore

/** Task-local spill storage. Heap budgets control caching and flushing, never application capacity. */
internal class LifecycleMetadataStore(
    file: File? = null,
    private val availableHeapBytes: () -> Long = {
        val runtime = Runtime.getRuntime()
        runtime.maxMemory() - (runtime.totalMemory() - runtime.freeMemory())
    },
) : AutoCloseable {
    private val store = MVStore.Builder().autoCommitDisabled().apply {
        if (file != null) fileName(file.absolutePath)
    }.open()
    private val onDisk = file != null
    private val headers = store.openMap<String, ByteArray>("headers")
    private var mapNumber = 0L

    init { maintainBudget() }

    fun header(name: String): LifecycleClassHeader? {
        maintainBudget()
        return headers[name]?.let { LifecycleHeaderCodec.decode(name, it) }
    }

    fun put(header: LifecycleClassHeader) {
        headers[header.name] = LifecycleHeaderCodec.encode(header)
        maintainBudget()
    }

    fun headers(): Sequence<LifecycleClassHeader> = headers.keyIterator(null).asSequence().map { name ->
        checkNotNull(header(name))
    }

    fun <T> newMap(): MVMap<String, T> = store.openMap("work-${mapNumber++}")

    fun maintainBudget() {
        if (!onDisk) return
        val available = availableHeapBytes().coerceAtLeast(0)
        val cacheKiB = (available / CACHE_HEAP_SHARE / KIB).coerceIn(KIB, Int.MAX_VALUE.toLong()).toInt()
        // H2 2.3.232 MVStore.setCacheSize(kb) divides by 1024 before calling FileStore;
        // getCacheSize() returns MiB. Use explicit methods to expose the asymmetric units.
        if (store.getCacheSize() != cacheKiB / KIB.toInt()) store.setCacheSize(cacheKiB)
        if (store.hasUnsavedChanges() && store.unsavedMemory.toLong() >= available / WRITE_HEAP_SHARE) store.commit()
    }

    internal val cacheMiB: Int get() = store.cacheSize

    override fun close() = store.closeImmediately()

    private companion object {
        const val KIB = 1024L
        const val CACHE_HEAP_SHARE = 32
        const val WRITE_HEAP_SHARE = 64
    }
}

private object LifecycleHeaderCodec {
    fun encode(header: LifecycleClassHeader): ByteArray = ByteArrayOutputStream().also { bytes ->
        DataOutputStream(bytes).use { out ->
            out.writeBoolean(header.superName != null)
            header.superName?.let { out.writeText(it) }
            out.writeInt(header.access)
            out.writeBoolean(header.programClass)
            out.writeBoolean(header.hasReferenceFields)
            out.writeStrings(header.interfaces)
            out.writeStrings(header.annotations)
            out.writeMethods(header.callbackAccess)
            out.writeMethods(header.accessorMethods)
        }
    }.toByteArray()

    fun decode(name: String, bytes: ByteArray): LifecycleClassHeader = DataInputStream(ByteArrayInputStream(bytes)).use { input ->
        val parent = if (input.readBoolean()) input.readText() else null
        val access = input.readInt()
        val program = input.readBoolean()
        val fields = input.readBoolean()
        val interfaces = input.readStrings()
        val annotations = input.readStrings().toSet()
        LifecycleClassHeader(name, parent, interfaces, access, input.readMethods(), program,
            annotations, input.readMethods(), fields)
    }

    private fun DataOutputStream.writeText(value: String) {
        val bytes = value.toByteArray(Charsets.UTF_8)
        writeInt(bytes.size)
        write(bytes)
    }
    private fun DataInputStream.readText(): String = ByteArray(readInt()).also(::readFully).toString(Charsets.UTF_8)
    private fun DataOutputStream.writeStrings(values: Collection<String>) {
        writeInt(values.size)
        values.forEach { writeText(it) }
    }
    private fun DataInputStream.readStrings(): List<String> = List(readInt()) { readText() }
    private fun DataOutputStream.writeMethods(values: Map<String, Int>) {
        writeInt(values.size)
        values.forEach { (name, flags) -> writeText(name); writeInt(flags) }
    }
    private fun DataInputStream.readMethods(): Map<String, Int> = buildMap {
        repeat(readInt()) { put(readText(), readInt()) }
    }
}
