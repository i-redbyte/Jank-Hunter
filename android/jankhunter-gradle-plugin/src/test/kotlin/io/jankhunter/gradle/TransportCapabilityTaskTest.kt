package io.jankhunter.gradle

import java.util.jar.JarEntry
import java.util.jar.JarOutputStream
import org.gradle.testfixtures.ProjectBuilder
import org.junit.Assert.assertEquals
import org.junit.Test

class TransportCapabilityTaskTest {
    @Test
    fun detectsHelperAbiAndInvalidatesWhenDependencyChanges() {
        val project = ProjectBuilder.builder().build()
        val task = project.tasks.register("capability", ResolveJankHunterTransportCapabilityTask::class.java).get()
        val jar = project.file("helper.jar")
        fun writeJar(withAbi: Boolean, withCodec: Boolean = true) {
            JarOutputStream(jar.outputStream()).use { output ->
                if (withCodec) {
                    output.putNextEntry(JarEntry("okhttp3/internal/http1/Http1Codec.class"))
                    output.write(byteArrayOf(1))
                    output.closeEntry()
                }
                if (withAbi) {
                    output.putNextEntry(JarEntry(OkHttpTransportClassVisitor.OWNER_ABI + ".class"))
                    output.write(byteArrayOf(1))
                    output.closeEntry()
                }
            }
        }
        task.supportClasspath.from(jar)
        task.outputFile.set(project.layout.buildDirectory.file("capability.txt"))
        writeJar(true)
        task.resolve()
        assertEquals(TransportCapability.AVAILABLE.encoded, task.outputFile.get().asFile.readText())
        writeJar(true, withCodec = false)
        task.resolve()
        assertEquals(TransportCapability.UNSUPPORTED_CODEC.encoded, task.outputFile.get().asFile.readText())
        writeJar(false)
        task.resolve()
        assertEquals(TransportCapability.MISSING_HELPER.encoded, task.outputFile.get().asFile.readText())
    }
}
