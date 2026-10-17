package io.jankhunter.gradle

import java.net.URLClassLoader
import java.nio.file.Files
import java.security.DigestInputStream
import java.security.MessageDigest
import kotlin.Metadata

/** Reader Kotlin stays behind a platform-parent loader and a primitive/JDK-only bridge. */
internal class IsolatedMetadataReader private constructor(
    private val loader: URLClassLoader,
    private val folder: java.io.File,
) : MetadataReaderBackend {
    private val entry = loader.loadClass("io.jankhunter.reader.IsolatedMetadataIndex").getMethod(
        "read", Int::class.javaPrimitiveType, IntArray::class.java, Array<String>::class.java,
        Array<String>::class.java, String::class.java, String::class.java, Int::class.javaPrimitiveType,
    )

    override fun decode(metadata: Metadata): Map<*, *> = entry.invoke(null, metadata.kind,
        metadata.metadataVersion, metadata.data1, metadata.data2, metadata.extraString,
        metadata.packageName, metadata.extraInt) as Map<*, *>

    override fun close() {
        try { loader.close() }
        finally { check(folder.deleteRecursively()) { "Cannot remove metadata reader resources" } }
    }

    companion object {
        @Volatile private var registry = newRegistry()

        fun read(metadata: Metadata): MetadataReaderResult = registry.read(metadata)

        @Synchronized fun release() {
            try { registry.close() }
            finally { registry = newRegistry() }
        }

        private fun newRegistry() = MetadataReaderRegistry(MetadataReaderCapabilities::bundled, ::create)

        private fun create(descriptor: MetadataReaderDescriptor): IsolatedMetadataReader {
            val folder = Files.createTempDirectory("jankhunter-metadata-reader").toFile()
            try {
                val urls = descriptor.jars.map { (name, expectedDigest) ->
                    val file = folder.resolve(name)
                    val digest = MessageDigest.getInstance("SHA-256")
                    checkNotNull(IsolatedMetadataReader::class.java.getResourceAsStream(
                        MetadataReaderCapabilities.RESOURCE_PREFIX + name,
                    )).use { input ->
                        DigestInputStream(input, digest).use { checkedInput ->
                            file.outputStream().use(checkedInput::copyTo)
                        }
                    }
                    if (digest.digest().joinToString("") { "%02x".format(it) } != expectedDigest) {
                        throw PermanentMetadataReaderFailure("Metadata reader package digest mismatch: $name")
                    }
                    file.toURI().toURL()
                }.toTypedArray()
                val loader = URLClassLoader(urls, ClassLoader.getPlatformClassLoader())
                return try { IsolatedMetadataReader(loader, folder) }
                catch (failure: Throwable) { loader.close(); throw failure }
            } catch (failure: Throwable) {
                folder.deleteRecursively()
                throw failure
            }
        }
    }
}
