package io.jankhunter.gradle

import java.io.InputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

internal fun writeCausalArchiveEntry(
    output: ZipOutputStream,
    name: String,
    open: () -> InputStream,
    transformClass: ((ByteArray) -> ByteArray)? = null,
) {
    output.putNextEntry(ZipEntry(name).apply { time = 0L })
    open().use { input ->
        if (transformClass == null) input.copyTo(output)
        else output.write(transformClass(input.readBytes()))
    }
    output.closeEntry()
}
