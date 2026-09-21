package io.jankhunter.runtime.internal.io

import java.io.File

internal fun wireGolden(name: String): ByteArray {
    val workingDirectory = requireNotNull(System.getProperty("user.dir"))
    var directory = File(workingDirectory).absoluteFile
    repeat(6) {
        val fixture = directory.resolve("wire/testdata/$name")
        if (fixture.isFile) return fixture.readBytes()
        directory = directory.parentFile ?: error("Cannot locate wire fixtures above $workingDirectory")
    }
    error("Cannot locate wire/testdata/$name from $workingDirectory")
}
