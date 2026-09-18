package io.jankhunter.gradle

import java.io.File
import java.io.RandomAccessFile
import java.nio.file.Files
import java.util.zip.ZipInputStream

/** No archive directory or application-wide bytecode collection is materialized in heap. */
internal object LifecycleClassInputs {
    fun forEach(file: File, consume: (ByteArray) -> Unit) {
        check(file.exists()) { "Lifecycle class input is not ready: $file" }
        if (file.isDirectory) {
            Files.walk(file.toPath()).use { paths ->
                paths.filter { Files.isRegularFile(it) && isClass(file.toPath().relativize(it).toString()) }
                    .forEach { consume(Files.readAllBytes(it)) }
            }
        } else {
            ZipInputStream(file.inputStream().buffered()).use { zip ->
                while (true) {
                    val entry = zip.nextEntry ?: break
                    if (!entry.isDirectory && isClass(entry.name)) consume(zip.readBytes())
                    zip.closeEntry()
                }
            }
        }
    }

    private fun isClass(name: String): Boolean = name.endsWith(".class") &&
        name != "module-info.class" && !name.startsWith("META-INF/")
}

/** Sequential payload storage; the sorted disk index keeps output independent of input entry order. */
internal class LifecycleClassSpool(file: File, private val index: LifecycleClassIndex) : AutoCloseable {
    private val output = RandomAccessFile(file, "rw")
    private val offsets = index.newWorkMap<Long>()

    fun add(bytes: ByteArray) {
        val name = index.add(bytes, true)
        val offset = output.length()
        output.seek(offset)
        output.writeInt(bytes.size)
        output.write(bytes)
        offsets[name] = offset
        index.maintainBudget()
    }

    fun read(name: String): ByteArray {
        output.seek(checkNotNull(offsets[name]) { "Missing lifecycle bytecode: $name" })
        return ByteArray(output.readInt()).also(output::readFully)
    }

    override fun close() = output.close()
}
